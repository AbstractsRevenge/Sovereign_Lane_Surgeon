// Copyright 2026 The Android Open Source Project
// Copyright 2026 Sovereign Lane Surgeon
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// devicerevival.go — stock device revival: seed a device family that was DROPPED from (or never
// existed in) the target AOSP tree, mirroring it VERBATIM from a source tree instead of forking it
// into a "_<lane>" parallel. There is no stock parallel to drop here — the device simply didn't
// exist in the target tree — so none of the lane machinery (finder routing, soong_config namespaces,
// "_<lane>" product suffixing, the deviceProductTmpl/androidProductsMkTmpl TODO-templates) applies.
// `create -stock -source-root <tree> -devices <a,b> -out <tree>` selects this path instead of
// writeScaffold. Informed directly by reviving Panther/Cheetah by hand first (2026-09; see
// physical_device_compiling_android-17.md) — every mechanical step here mirrors a step done manually
// there, generalized.
//
// v0.4.0: with -factory-images-root the vendor partition images are wired through the stock
// self-extractor mechanism and their contents unpacked (vendorutils.go, debugfs first, no root);
// with -release the per-device kernel prebuilt dir is assembled from the same image
// (kernelprebuilt.go) — the piece the embedded AOSP 15 bundle cannot carry — and, unless
// -kernel-version overrides it, TARGET_LINUX_KERNEL_VERSION is set from the kernel the image
// actually carries (6.1 for the CP2A Pixel 7 family; the earlier 6.12 assumption was wrong).
//
// "Fully required" is derived, not listed: after the family dirs land, every device/google and
// hardware/google subtree their makefiles and Blueprints reference is mirrored too, transitively
// (referencedGoogleSubtrees) — SoC dirs, sepolicy, gs-common, gchips/graphics/pixel — as long as a
// source has it; upstream git projects are left to upstream (mirrorFromBestSource's guard) and
// proprietary trees AOSP never ships are reported. Then the target-release compatibility pass
// (targetcompat.go) rewrites, in the mirrored subtrees only, the idioms the target tree is probed
// to reject. Measured result: `create -stock -release cp2a` + `m nothing` green on
// android-17.0.0_r1 for panther/cheetah/lynx/tangorpro with no hand edit (2026-09-02).

// stockArgs bundles cmdCreate's -stock-mode flags (kept separate from the interactive/-name lane
// flow in cmdCreate for readability).
type stockArgs struct {
	out               string
	sourceRoot        string
	devices           string
	kernelVersion     string
	hwSubtrees        string
	factoryImagesRoot string
	release           string
}

// preSeedFromEmbedded ensures device/google/<family> exists under outRoot before lane device
// resolution runs (writeDeviceProducts, deviceproduct.go), by materializing it from the embedded
// asset bundle when it's missing there entirely — so LANE creation, like stock revival, needs no
// external AOSP tree for a device this bundle covers. A no-op when the family is already present
// under -out (never overwrites/duplicates real content) or when the product isn't one the embedded
// bundle knows about (resolveDeviceFromFS leaves Resolved false). Either way — freshly seeded or
// already present — runs the bp-parity check (bpparity.go) against the embedded reference: a
// pre-existing family dir may predate this tool and carry stale/incomplete .bp content (the exact
// gchips/graphics fabrication bug this project hit), which a fresh mirror can't, by construction.
func preSeedFromEmbedded(outRoot, product string) {
	if outRoot == "" {
		return
	}
	res := resolveDeviceFromFS(embeddedFS, product)
	if !res.Resolved {
		return
	}
	famRel := filepath.Join("device", "google", res.Family)
	if info, err := os.Stat(filepath.Join(outRoot, famRel)); err != nil || !info.IsDir() {
		if n, err := materializeEmbedded(filepath.ToSlash(famRel), outRoot); err == nil && n > 0 {
			fmt.Printf("  seeded device/google/%s/ from the embedded asset bundle (%d files — no external AOSP tree needed)\n", res.Family, n)
		}
	}
	printParityFindings(checkBPParityForTree(embeddedFS, filepath.ToSlash(famRel), outRoot))
}

// cmdCreateStock validates stock-mode flags, builds one LaneConfig (Stock: true) for every
// requested device, and runs writeStockScaffold. Unlike lane mode, there is no dry-run/interactive
// path. -source-root is OPTIONAL (not required, unlike an early revision of this command): the
// embedded asset bundle (embeddedassets.go) covers device/google/{pantah,lynx,tangorpro,gs101,
// gs201,gs-common}(-sepolicy) + hardware/google/{gchips,graphics,pixel,pixel-sepolicy} with zero
// external tree needed at all — "one stop shop", per the project goal. -source-root remains
// available for anything the embedded bundle doesn't cover (another device family entirely) or to
// override with a fresher/live tree; when given, it's tried FIRST, falling back to the embedded
// bundle for any path it doesn't have.
func cmdCreateStock(a stockArgs) int {
	if strings.TrimSpace(a.out) == "" {
		fmt.Fprintln(os.Stderr, "create -stock: -out <aosp-root> is required")
		return 2
	}
	var devs []string
	for _, d := range strings.Split(a.devices, ",") {
		if s := strings.TrimSpace(d); s != "" {
			devs = append(devs, s)
		}
	}
	if len(devs) == 0 {
		fmt.Fprintln(os.Stderr, "create -stock: -devices <name,...> is required")
		return 2
	}
	var hw []string
	for _, h := range strings.Split(a.hwSubtrees, ",") {
		if s := strings.TrimSpace(h); s != "" {
			hw = append(hw, s)
		}
	}
	cfg := LaneConfig{
		Stock:             true,
		SourceRoot:        a.sourceRoot,
		Devices:           devs,
		KernelVersion:     a.kernelVersion,
		HWSubtrees:        hw,
		FactoryImagesRoot: a.factoryImagesRoot,
		Release:           a.release,
	}
	return writeStockScaffold(cfg, a.out)
}

// neutralizedByBak reports whether target has been deliberately disabled via a "<file>.bak"
// sibling — the pattern used when converting a blocking Android.mk to Android.bp (see
// hardware/google/graphics/common/{,libhwc2.1/,hwc3/}Android.mk.bak from this project's own Phase 1
// fixes). A mirror step must treat that the same as an already-existing file: copying a fresh
// Android.mk back in over a deliberately-neutralized one reintroduces the exact duplicate-module
// collision the neutralization existed to prevent in the first place (caught 2026-09-01 on the real
// android-17.0.0_r1 tree: a stock-revival run silently resurrected 3 Android.mk files whose
// LOCAL_MODULE collided with modules already hand-converted into a sibling Android.bp).
func neutralizedByBak(target string) bool {
	_, err := os.Stat(target + ".bak")
	return err == nil
}

// mirrorStockTree recursively copies sourceRoot/relPath onto outRoot/relPath verbatim: same path, no lane
// suffix, no exclusions beyond .git/.repo, no relicensing, no collision-prone-.bp dropping — unlike
// copyDeviceFamilyTree, there is no lane/stock duality to resolve, so none of that logic applies.
// BoardConfig.mk-containing dirs are INCLUDED (copyDeviceFamilyTree deliberately skips them because
// "the lane shares the stock board" — there is no stock board to share here). No-clobber (an
// existing target file always wins — safe to re-run against a tree that's already partially
// revived, and never overwrites a hand-tuned fix already applied mid-bring-up), symlink-safe.
func mirrorStockTree(sourceRoot, outRoot, relPath string) (copied int, err error) {
	src := filepath.Join(sourceRoot, relPath)
	info, serr := os.Stat(src)
	if serr != nil {
		return 0, fmt.Errorf("source path %s not found under -source-root", relPath)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("source path %s is not a directory", relPath)
	}
	dst := filepath.Join(outRoot, relPath)
	walkErr := filepath.Walk(src, func(p string, fi os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if base := filepath.Base(p); base == ".git" || base == ".repo" {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			link, lerr := os.Readlink(p)
			if lerr != nil {
				return nil
			}
			if _, lstatErr := os.Lstat(target); lstatErr == nil {
				return nil // no-clobber
			}
			if merr := os.MkdirAll(filepath.Dir(target), 0o755); merr != nil {
				return merr
			}
			if os.Symlink(link, target) == nil {
				copied++
			}
			return nil
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		if _, statErr := os.Stat(target); statErr == nil || neutralizedByBak(target) {
			return nil // no-clobber
		}
		if mirrorSkipsMakefile(outRoot, filepath.ToSlash(filepath.Join(relPath, rel)), os.DirFS(sourceRoot)) {
			return nil // denylisted by the target (overlay-replaced or an empty wrapper) — never lands
		}
		if cerr := copyFile(p, target); cerr != nil {
			return cerr
		}
		copied++
		return nil
	})
	return copied, walkErr
}

// mirrorFromBestSource mirrors relPath into outRoot, preferring c.SourceRoot (external, possibly
// fresher) when it's set AND actually has relPath, falling back to the embedded asset bundle
// otherwise. source is "source-root" / "embedded" / "" (not found anywhere) for caller logging.
func mirrorFromBestSource(c LaneConfig, outRoot, relPath string) (copied int, source string, err error) {
	// A target that is a git checkout is owned by upstream, not by this bundle: a no-clobber mirror
	// would still ADD every file the bundle has and the checkout doesn't (e.g. AOSP 15's
	// Android.mk files into a hardware/google/graphics/common that was replaced by upstream main,
	// whose Soong conversion deleted exactly those makefiles) and resurrect the collisions the
	// checkout resolved. Observed hazard, 2026-09-02; the mirror refuses instead.
	if _, gerr := os.Stat(filepath.Join(outRoot, relPath, ".git")); gerr == nil {
		return 0, "", fmt.Errorf("%s is a git checkout under -out — left untouched (upstream owns it; the bundle would re-add files it removed)", relPath)
	}
	if c.SourceRoot != "" {
		if info, serr := os.Stat(filepath.Join(c.SourceRoot, relPath)); serr == nil && info.IsDir() {
			n, mErr := mirrorStockTree(c.SourceRoot, outRoot, relPath)
			return n, "source-root", mErr
		}
	}
	if hasEmbeddedPath(filepath.ToSlash(relPath)) {
		n, mErr := materializeEmbedded(filepath.ToSlash(relPath), outRoot)
		return n, "embedded", mErr
	}
	return 0, "", fmt.Errorf("%s not found under -source-root or in the embedded bundle", relPath)
}

// resolveDeviceFromFS derives family + SoC for product by reading fsys (an fs.FS — either a real
// external tree via os.DirFS, or the embedded bundle): which device/google/<family>/
// AndroidProducts.mk lists aosp_<product>.mk (-> family), and which device/google/<soc>/
// aosp_common.mk that stock product inherits (-> SoC). fs.FS paths are always forward-slashed
// (io/fs requirement) — "path", not "filepath", throughout.
func resolveDeviceFromFS(fsys fs.FS, product string) deviceResolution {
	res := deviceResolution{Product: product, ProductTitle: title(product), Family: product}
	matches, _ := fs.Glob(fsys, "device/google/*/AndroidProducts.mk")
	needle := "aosp_" + product + ".mk"
	for _, m := range matches {
		b, err := fs.ReadFile(fsys, m)
		if err != nil || !strings.Contains(string(b), needle) {
			continue
		}
		res.Family = path.Base(path.Dir(m))
		stock := path.Join(path.Dir(m), needle)
		if sb, serr := fs.ReadFile(fsys, stock); serr == nil {
			res.SoC = extractSoCInherit(string(sb))
			res.Resolved = true
		}
		break
	}
	return res
}

// resolveDeviceCrossTree derives family + SoC for product, trying sourceRoot first (when given —
// same dual-source precedence as mirrorFromBestSource), then the embedded asset bundle. Needed
// because a net-new device (lynx, tangorpro) has nothing under outRoot to resolve against yet;
// that's the entire point of mirroring it in.
func resolveDeviceCrossTree(sourceRoot, product string) deviceResolution {
	if sourceRoot != "" {
		if info, err := os.Stat(sourceRoot); err == nil && info.IsDir() {
			if res := resolveDeviceFromFS(os.DirFS(sourceRoot), product); res.Resolved {
				return res
			}
		}
	}
	return resolveDeviceFromFS(embeddedFS, product)
}

// reconcileKernelVersion sets TARGET_LINUX_KERNEL_VERSION to version in each requested product's
// device/google/<family>/aosp_<product>.mk, PROVIDED it already declares the variable (string-scan,
// no regex — HARD RULE 3 kept even here). The value should be the kernel the device's factory image
// carries (writeStockScaffold derives it from the assembled kernel dir: CP2A Pixel 7 family = 6.1);
// -kernel-version overrides. The kernel BINARY comes from the factory image (kernelprebuilt.go),
// never from an older AOSP tree.
//
// ⚠️ products MUST be the exact requested device list, NOT every aosp_*.mk glob-matched under the
// family dir. A multi-product family (e.g. pantah: panther, cheetah, AND the unrequested dev boards
// cloudripper/ravenclaw) ships sibling products' aosp_*.mk in the SAME directory — a blanket glob
// silently reversion those out-of-scope siblings too (caught 2026-09-01 on the real android-17.0.0_r1
// tree: reviving panther+cheetah alone bumped aosp_cloudripper.mk/aosp_ravenclaw.mk from 5.10 to 6.12,
// devices nobody asked to touch). Reviving a device must never mutate a sibling nobody named.
func reconcileKernelVersion(outRoot, family string, products []string, version string) (changed int, err error) {
	const key = "TARGET_LINUX_KERNEL_VERSION"
	newLine := key + " := " + version
	for _, product := range products {
		m := filepath.Join(outRoot, "device", "google", family, "aosp_"+product+".mk")
		b, rerr := os.ReadFile(m)
		if rerr != nil {
			continue // no aosp_<product>.mk, or product uses a differently-named product file
		}
		lines := strings.Split(string(b), "\n")
		found, dirty := false, false
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), key) {
				found = true
				if strings.TrimSpace(line) != newLine {
					lines[i] = newLine
					dirty = true
				}
			}
		}
		if !found || !dirty {
			continue
		}
		if werr := os.WriteFile(m, []byte(strings.Join(lines, "\n")), 0o644); werr != nil {
			return changed, werr
		}
		changed++
	}
	return changed, nil
}

// ─── v0.4.0: Vendor Image Content Extraction ───

// extractVendorBlobsFromFactory is the complete vendor blob pipeline for one device: wire the
// partition images through the stock self-extractor mechanism (vendorblobs.go), then unpack
// vendor.img / vendor_dlkm.img / system_dlkm.img contents (vendorutils.go, no root needed when
// debugfs is present).
func extractVendorBlobsFromFactory(outRoot, family, device, factoryDir string) error {
	if err := wireVendorBlobs(outRoot, family, device, factoryDir); err != nil {
		return fmt.Errorf("wireVendorBlobs: %w", err)
	}
	if _, err := extractVendorImages(device, factoryDir, outRoot); err != nil {
		return fmt.Errorf("extractVendorImages: %w", err)
	}
	return nil
}

// ─── End v0.4.0 enhancement ───

// writeStockScaffold mirrors one or more device families VERBATIM into outRoot — the stock-revival
// counterpart to writeScaffold. Per device: resolve family+SoC (trying c.SourceRoot first when set,
// then the embedded asset bundle); once per family, mirror device/google/<family> (BoardConfig.mk
// and all), device/google/<family>-kernels, and device/google/<family>-sepolicy verbatim if
// available. No templating: the mirrored aosp_<product>.mk/AndroidProducts.mk/device-<product>.mk
// carry over exactly as upstream shipped them, so they're already complete (a mirrored family's
// AndroidProducts.mk needs no registerDeviceProduct-style merge — the whole family, and every
// product it lists, comes over together). c.SourceRoot may be empty — the embedded bundle alone
// covers pantah/lynx/tangorpro/gs101/gs201/gs-common + the gchips/graphics/pixel hardware subtrees.
//
// v0.4.0 enhancement: also extracts vendor image contents when -factory-images-root is provided.
func writeStockScaffold(c LaneConfig, outRoot string) int {
	if strings.TrimSpace(outRoot) == "" {
		fmt.Fprintln(os.Stderr, "create -stock: -out <aosp-root> is required")
		return 2
	}
	srcDesc := c.SourceRoot
	if srcDesc == "" {
		srcDesc = "the embedded asset bundle"
	}
	fmt.Printf("Reviving %d device(s) from %s into %s\n\n", len(c.Devices), srcDesc, outRoot)

	rc := 0
	mirroredFamilies := map[string]bool{}
	mirrored := map[string]bool{} // every repo-relative subtree this run owns (forward-slashed)
	for _, product := range c.Devices {
		res := resolveDeviceCrossTree(c.SourceRoot, product)
		if res.Resolved {
			fmt.Printf("  device %s → family=%s soc=%s\n", product, res.Family, res.SoC)
		} else {
			fmt.Printf("  device %s → UNRESOLVED under -source-root or the embedded bundle (family assumed %q, SoC unknown)\n", product, res.Family)
		}
		if mirroredFamilies[res.Family] {
			continue
		}
		mirroredFamilies[res.Family] = true

		famRel := filepath.Join("device", "google", res.Family)
		n, src, mErr := mirrorFromBestSource(c, outRoot, famRel)
		if mErr != nil {
			fmt.Fprintf(os.Stderr, "  ! mirror %s: %v\n", famRel, mErr)
			rc = 1
			continue
		}
		fmt.Printf("  mirrored %d file(s) from %s: device/google/%s/\n", n, src, res.Family)
		printParityFindings(checkBPParityForTree(bestSourceFS(c, src), filepath.ToSlash(famRel), outRoot))
		mirrored[filepath.ToSlash(famRel)] = true

		for _, suffix := range []string{"-kernels", "-sepolicy"} {
			rel := famRel + suffix
			n, src, mErr := mirrorFromBestSource(c, outRoot, rel)
			if mErr != nil {
				continue // this family has no <family>-kernels or <family>-sepolicy sibling dir anywhere
			}
			fmt.Printf("  mirrored %d file(s) from %s: device/google/%s%s/\n", n, src, res.Family, suffix)
			printParityFindings(checkBPParityForTree(bestSourceFS(c, src), filepath.ToSlash(rel), outRoot))
			mirrored[filepath.ToSlash(rel)] = true
		}

		if c.KernelVersion != "" {
			// Scope to ONLY the requested devices that resolve to this family — never every
			// aosp_*.mk the family dir happens to ship (see reconcileKernelVersion's doc comment).
			var familyProducts []string
			for _, p := range c.Devices {
				if r := resolveDeviceCrossTree(c.SourceRoot, p); r.Family == res.Family {
					familyProducts = append(familyProducts, p)
				}
			}
			n, kErr := reconcileKernelVersion(outRoot, res.Family, familyProducts, c.KernelVersion)
			if kErr != nil {
				fmt.Fprintf(os.Stderr, "  ! kernel version reconcile for %s: %v\n", res.Family, kErr)
				rc = 1
			} else if n > 0 {
				fmt.Printf("  set TARGET_LINUX_KERNEL_VERSION := %s in %d product mk(s) for %s\n", c.KernelVersion, n, res.Family)
			}
		}
	}

	for _, hw := range c.HWSubtrees {
		n, src, mErr := mirrorFromBestSource(c, outRoot, hw)
		if mErr != nil {
			fmt.Fprintf(os.Stderr, "  ! mirror %s: %v\n", hw, mErr)
			rc = 1
			continue
		}
		fmt.Printf("  mirrored %d file(s) from %s: %s/\n", n, src, hw)
		printParityFindings(checkBPParityForTree(bestSourceFS(c, src), filepath.ToSlash(hw), outRoot))
		mirrored[filepath.ToSlash(hw)] = true
	}

	// Everything the mirrored trees reference, transitively (SoC dirs, sepolicy, gs-common, the
	// hardware/google HAL trees), from whichever source has it.
	mirrorReferencedSubtrees(c, outRoot, mirrored)

	// Cross-tag reconciliations (reconcile.go): a family from another AOSP tag than the shared
	// subtrees may include a makefile the shared tag removed; listed lines go when the path is absent.
	if rep, rerr := applyReconciliations(outRoot); rerr != nil {
		fmt.Fprintf(os.Stderr, "  ! %v\n", rerr)
		rc = 1
	} else {
		for _, line := range rep {
			fmt.Printf("  reconciled: %s\n", line)
		}
	}

	roots := make([]string, 0, len(mirrored))
	for d := range mirrored {
		roots = append(roots, d)
	}
	sort.Strings(roots)
	printCompatReport(applyTargetCompat(outRoot, roots))

	// v0.4.0 enhancement: Extract vendor image contents if factory images root is provided
	if c.FactoryImagesRoot != "" {
		fmt.Println("\nExtracting vendor image contents from factory images...")
		for _, product := range c.Devices {
			res := resolveDeviceCrossTree(c.SourceRoot, product)
			factoryDir := filepath.Join(c.FactoryImagesRoot, product)

			// Check if factory directory exists
			if _, err := os.Stat(factoryDir); err != nil {
				fmt.Fprintf(os.Stderr, "  ! factory directory for %s not found: %v\n", product, err)
				rc = 1
				continue
			}

			// Use the enhanced extraction function
			if err := extractVendorBlobsFromFactory(outRoot, res.Family, product, factoryDir); err != nil {
				fmt.Fprintf(os.Stderr, "  ! vendor extraction for %s: %v\n", product, err)
				rc = 1
			}
			// Kernel prebuilt dir (kernelprebuilt.go): the release names the dir, the factory image
			// supplies its contents. Runs even if the mount-based extraction above failed — the
			// first-stage modules and boot/dtbo need no privileges; what is missing is reported.
			if c.Release == "" {
				fmt.Printf("  ~ kernel prebuilt dir for %s skipped: pass -release <rel> (e.g. cp2a) to assemble RELEASE_KERNEL_%s_DIR\n", product, strings.ToUpper(product))
				continue
			}
			k, r := runAssembleKernel(outRoot, c.Release, product, factoryDir)
			if r != 0 {
				rc = 1
				continue
			}
			// The kernel version the product declares comes from the kernel the image carries,
			// unless -kernel-version said otherwise (the flag was honoured per family above).
			if c.KernelVersion == "" && k != nil && k.KernelRelease != "" {
				if ver := kernelMajorMinor(k.KernelRelease); ver != "" {
					n, kErr := reconcileKernelVersion(outRoot, res.Family, []string{product}, ver)
					if kErr != nil {
						fmt.Fprintf(os.Stderr, "  ! kernel version reconcile for %s: %v\n", product, kErr)
						rc = 1
					} else if n > 0 {
						fmt.Printf("  set TARGET_LINUX_KERNEL_VERSION := %s in aosp_%s.mk (from the image's kernel %s)\n", ver, product, k.KernelRelease)
					}
				}
			}
		}
	}

	return rc
}

// mirrorReferencedSubtrees mirrors, transitively, every device/google and hardware/google subtree
// the already-mirrored dirs reference (referencedGoogleSubtrees) that -source-root or the embedded
// bundle can supply, adding each to mirrored and scanning it in turn. A referenced subtree that is
// a git checkout under -out belongs to upstream and is left alone; one that no source has is
// reported once (proprietary vendor trees, or a family the bundle does not carry yet).
func mirrorReferencedSubtrees(c LaneConfig, outRoot string, mirrored map[string]bool) {
	queue := make([]string, 0, len(mirrored))
	for d := range mirrored {
		queue = append(queue, d)
	}
	sort.Strings(queue)
	checked := map[string]bool{}
	var upstream, unavailable []string
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		for _, ref := range referencedGoogleSubtrees(filepath.Join(outRoot, filepath.FromSlash(d))) {
			if mirrored[ref] || checked[ref] {
				continue
			}
			checked[ref] = true
			n, src, mErr := mirrorFromBestSource(c, outRoot, filepath.FromSlash(ref))
			if mErr != nil {
				if _, gerr := os.Stat(filepath.Join(outRoot, filepath.FromSlash(ref), ".git")); gerr == nil {
					upstream = append(upstream, ref)
				} else {
					unavailable = append(unavailable, ref)
				}
				continue
			}
			fmt.Printf("  mirrored %d file(s) from %s: %s/ (referenced by %s)\n", n, src, ref, d)
			printParityFindings(checkBPParityForTree(bestSourceFS(c, src), ref, outRoot))
			mirrored[ref] = true
			queue = append(queue, ref)
		}
	}
	if len(upstream) > 0 {
		fmt.Printf("  = referenced, owned by the target tree (git checkouts, left untouched): %s\n", strings.Join(upstream, " "))
	}
	if len(unavailable) > 0 {
		fmt.Printf("  ~ referenced, no source has them (proprietary or not bundled): %s\n", strings.Join(unavailable, " "))
	}
}
