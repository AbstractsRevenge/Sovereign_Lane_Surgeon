// Copyright 2026 Terrance Leverette (AbstractsRevenge)
// Sovereign Lane Surgeon: https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon
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
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	parser "github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/internal/blueprint/parser"
)

// seedverify.go — `verify-seed`: does the tree `create -stock` just produced actually satisfy the
// invariants the build depends on? Run automatically at the end of every `create -stock`.
//
// WHY: every defect this port hit was a seed that LOOKED complete. The vendor glue sat one
// directory away from where the device tree includes it, so `BOARD_PREBUILT_VENDORIMAGE` never
// loaded and super shipped without vendor. A blob landed flat while an Android.bp named it under
// lib64/, so Soong failed on a file that was right there. A symlink never arrived, so an include
// resolved nowhere. None of it is visible by reading the seed log, and `m nothing` is green
// through all of it — the cost lands 25 to 46 minutes into a full build, or at a black screen.
//
// Every check below is DERIVED from the tree, never a per-device list: the invariant is always
// "the file some other file names must exist", so the checks keep working for a device, family or
// release this code has never seen.

type seedCheck struct {
	Name, Status, Detail string
}

func (c seedCheck) String() string { return fmt.Sprintf("%-4s %-16s %s", c.Status, c.Name, c.Detail) }

// mkDirectives scans the makefiles under dir for every `vendor/google_devices/<device>/...` path,
// classified by the directive that names it. Make distinguishes the two, and so must this: a path
// named by `-include` or `inherit-product-if-exists` is OPTIONAL — Google's device trees list many
// vendor makefiles that exist only in an internal build, and their absence is the intended
// behaviour, not a defect (tegu names three such). A path named by a bare `include` or
// `inherit-product` is REQUIRED, and its absence stops the build.
func mkDirectives(dir, device string) (required, optional []string) {
	needle := "vendor/google_devices/" + device + "/"
	req, opt := map[string]bool{}, map[string]bool{}
	walkFiles(dir, func(n string) bool { return strings.HasSuffix(n, ".mk") }, func(p string) {
		b, err := os.ReadFile(p)
		if err != nil {
			return
		}
		for _, raw := range strings.Split(string(b), "\n") {
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			isOptional := strings.Contains(line, "inherit-product-if-exists") || strings.HasPrefix(line, "-include")
			isRequired := !isOptional && (strings.Contains(line, "inherit-product") || strings.HasPrefix(line, "include"))
			if !isOptional && !isRequired {
				continue
			}
			for _, tok := range strings.Fields(line) {
				tok = strings.Trim(tok, "(),")
				if !strings.HasPrefix(tok, needle) || !strings.HasSuffix(tok, ".mk") {
					continue
				}
				if isOptional {
					opt[tok] = true
				} else {
					req[tok] = true
				}
			}
		}
	})
	for p := range req {
		required = append(required, p)
	}
	for p := range opt {
		if !req[p] {
			optional = append(optional, p)
		}
	}
	sort.Strings(required)
	sort.Strings(optional)
	return required, optional
}

// buildSystemEntryPoint reports whether a makefile is FOUND by the build system rather than
// included by another makefile: kati discovers every Android.mk and CleanSpec.mk, and product
// config discovers AndroidProducts.mk. Nothing names them, and nothing should.
func buildSystemEntryPoint(base string) bool {
	switch base {
	case "Android.mk", "AndroidProducts.mk", "CleanSpec.mk":
		return true
	}
	return false
}

// orphanVendorMakefiles returns the makefiles under vendor/google_devices/<device>/ that NO
// directive anywhere names — neither in the device family tree nor in the vendor tree itself.
//
// This is the check that catches a misplacement, and it is the one that mattered. The vendor glue
// bug put BoardConfigVendor.mk flat while every device tree includes it from proprietary/. The
// file existed, so "is it there?" answered yes. It is included by an -if-exists directive, so
// "is anything required missing?" answered no. Only "is this file reachable?" answers no, which
// is the truth: BOARD_PREBUILT_VENDORIMAGE never loaded and super shipped without vendor.
func orphanVendorMakefiles(outRoot, famDir, device string) []string {
	vendorDir := filepath.Join(outRoot, "vendor", "google_devices", device)
	named := map[string]bool{}
	for _, dir := range []string{famDir, vendorDir} {
		req, opt := mkDirectives(dir, device)
		for _, p := range append(req, opt...) {
			named[p] = true
		}
	}
	var orphans []string
	walkFiles(vendorDir, func(n string) bool { return strings.HasSuffix(n, ".mk") && !buildSystemEntryPoint(n) }, func(p string) {
		rel, err := filepath.Rel(outRoot, p)
		if err != nil {
			return
		}
		if !named[filepath.ToSlash(rel)] {
			orphans = append(orphans, filepath.ToSlash(rel))
		}
	})
	sort.Strings(orphans)
	return orphans
}

// bpSrcPaths returns the file paths a Blueprint's modules name in srcs-like properties, relative
// to the Blueprint's own directory. Parsed, not scanned.
func bpSrcPaths(bpPath string) []string {
	b, err := os.ReadFile(bpPath)
	if err != nil {
		return nil
	}
	file, errs := parser.Parse(bpPath, bytes.NewReader(b))
	if len(errs) > 0 {
		return nil
	}
	set := map[string]bool{}
	var walk func(v parser.Expression)
	walk = func(v parser.Expression) {
		switch e := v.(type) {
		case *parser.List:
			for _, x := range e.Values {
				walk(x)
			}
		case *parser.Map:
			for _, pr := range e.Properties {
				if pr.Name == "srcs" {
					collectStrings(pr.Value, set)
				} else {
					walk(pr.Value)
				}
			}
		}
	}
	for _, def := range file.Defs {
		m, ok := def.(*parser.Module)
		if !ok {
			continue
		}
		for _, pr := range m.Properties {
			if pr.Name == "srcs" {
				collectStrings(pr.Value, set)
				continue
			}
			walk(pr.Value)
		}
	}
	var out []string
	for p := range set {
		if p != "" && !strings.HasPrefix(p, ":") && !strings.Contains(p, "*") {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// copyFileSources returns the source paths of PRODUCT_COPY_FILES entries ("<src>:<dest>[:owner]").
func copyFileSources(mkPath, device string) []string {
	b, err := os.ReadFile(mkPath)
	if err != nil {
		return nil
	}
	prefix := "vendor/google_devices/" + device + "/"
	var out []string
	for _, tok := range strings.Fields(string(b)) {
		tok = strings.TrimSuffix(strings.TrimSpace(tok), "\\")
		if !strings.HasPrefix(tok, prefix) || !strings.Contains(tok, ":") {
			continue
		}
		out = append(out, strings.SplitN(tok, ":", 2)[0])
	}
	sort.Strings(out)
	return out
}

// verifySeed checks one seeded device. family may be "" (resolved from the tree).
func verifySeed(outRoot, device, family, release string) []seedCheck {
	var cs []seedCheck
	add := func(n, st, d string) { cs = append(cs, seedCheck{n, st, d}) }
	if family == "" {
		family = resolveDeviceCrossTree("", device).Family
	}
	famDir := filepath.Join(outRoot, "device", "google", family)
	if _, err := os.Stat(famDir); err != nil {
		add("family-tree", "FAIL", "device/google/"+family+" is not present under -out")
		return cs
	}
	add("family-tree", "PASS", "device/google/"+family)

	// 1. Bundle fidelity, in the tree that was actually written: every executable and symlink the
	//    manifests record for a mirrored subtree must be there. This is the check that turns a
	//    silent embed loss into a seed-time failure instead of a build-time one.
	mirrored := func(p string) bool {
		_, err := os.Stat(filepath.Join(outRoot, filepath.FromSlash(path0(p))))
		return err == nil
	}
	var missingExec, missingLink, danglingLink []string
	for p := range embeddedExecutables {
		if !mirrored(p) {
			continue
		}
		fi, err := os.Stat(filepath.Join(outRoot, filepath.FromSlash(p)))
		if err != nil || fi.Mode()&0o111 == 0 {
			missingExec = append(missingExec, p)
		}
	}
	for _, l := range bundleSymlinks {
		if !mirrored(l.Path) {
			continue
		}
		full := filepath.Join(outRoot, filepath.FromSlash(l.Path))
		if _, err := os.Lstat(full); err != nil {
			missingLink = append(missingLink, l.Path)
			continue
		}
		// A link whose target lies inside the bundle's own subtrees MUST resolve: that is the
		// compatibility-header case, and a build fails on it. A link that points out into the
		// wider AOSP tree (pixel's .clang-format -> build/soong/scripts/...) resolves only once
		// that tree is there, so its absence is a property of where the seed landed, not a defect.
		if _, err := os.Stat(full); err != nil && linkTargetIsInsideBundle(l) {
			danglingLink = append(danglingLink, l.Path)
		}
	}
	sort.Strings(missingExec)
	sort.Strings(missingLink)
	switch {
	case len(missingExec) > 0:
		add("exec-bits", "FAIL", fmt.Sprintf("%d file(s) lost their executable bit: %s", len(missingExec), strings.Join(trim5(missingExec), " ")))
	default:
		add("exec-bits", "PASS", "every executable the bundle records in the mirrored subtrees is 0755")
	}
	switch {
	case len(missingLink) > 0:
		add("symlinks", "FAIL", fmt.Sprintf("%d symlink(s) missing: %s", len(missingLink), strings.Join(trim5(missingLink), " ")))
	case len(danglingLink) > 0:
		add("symlinks", "FAIL", fmt.Sprintf("%d symlink(s) do not resolve: %s", len(danglingLink), strings.Join(trim5(danglingLink), " ")))
	default:
		add("symlinks", "PASS", "every symlink the bundle records in the mirrored subtrees exists and resolves")
	}

	// 2. Vendor wiring, only when a vendor tree exists (a seed without -factory-images-root has none).
	vendorDir := filepath.Join(outRoot, "vendor", "google_devices", device)
	if _, err := os.Stat(vendorDir); err != nil {
		add("vendor-glue", "SKIP", "no vendor/google_devices/"+device+" (seeded without -factory-images-root)")
		add("vendor-blobs", "SKIP", "no vendor tree")
	} else {
		// Two directions, because each catches what the other cannot.
		//   required-missing: a path a bare `include`/`inherit-product` names but nothing wrote.
		//   orphan: a vendor makefile that exists where NO directive names it — the glue bug.
		required, optional := mkDirectives(famDir, device)
		var missing []string
		for _, p := range required {
			if !fileExists(filepath.Join(outRoot, filepath.FromSlash(p))) {
				missing = append(missing, p)
			}
		}
		orphans := orphanVendorMakefiles(outRoot, famDir, device)
		var absentOptional int
		for _, p := range optional {
			if !fileExists(filepath.Join(outRoot, filepath.FromSlash(p))) {
				absentOptional++
			}
		}
		switch {
		case len(missing) > 0:
			add("vendor-glue", "FAIL", fmt.Sprintf("%d required vendor makefile(s) the device tree includes are not there: %s",
				len(missing), strings.Join(trim5(missing), " ")))
		case len(orphans) > 0:
			add("vendor-glue", "FAIL", fmt.Sprintf("%d vendor makefile(s) exist where nothing includes them — misplaced: %s",
				len(orphans), strings.Join(trim5(orphans), " ")))
		default:
			add("vendor-glue", "PASS", fmt.Sprintf("every vendor makefile present is included by the tree (%d required, %d optional of which %d legitimately absent)",
				len(required), len(optional), absentOptional))
		}
		// Every blob the vendor makefiles and Blueprints name must exist where they name it.
		propDir := filepath.Join(vendorDir, "proprietary")
		var missingBlobs []string
		for _, mk := range []string{"device-partial.mk", "device-vendor.mk", filepath.Join("proprietary", "device-partial.mk"), filepath.Join("proprietary", "device-vendor.mk")} {
			for _, src := range copyFileSources(filepath.Join(vendorDir, mk), device) {
				if !fileExists(filepath.Join(outRoot, filepath.FromSlash(src))) {
					missingBlobs = append(missingBlobs, src)
				}
			}
		}
		for _, bp := range []string{"Android.bp", "Android.mk"} {
			p := filepath.Join(propDir, bp)
			if !fileExists(p) {
				continue
			}
			for _, src := range bpSrcPaths(p) {
				if !fileExists(filepath.Join(propDir, filepath.FromSlash(src))) {
					missingBlobs = append(missingBlobs, "proprietary/"+src)
				}
			}
		}
		sort.Strings(missingBlobs)
		if len(missingBlobs) > 0 {
			add("vendor-blobs", "FAIL", fmt.Sprintf("%d blob(s) named by the vendor makefiles/Blueprints are absent: %s",
				len(missingBlobs), strings.Join(trim5(missingBlobs), " ")))
		} else {
			add("vendor-blobs", "PASS", "every blob the vendor makefiles and Blueprints name is where they name it")
		}
	}

	// 3. Kernel prebuilt directory the release names.
	if release == "" {
		add("kernel-dir", "SKIP", "no -release")
	} else if dir, err := releaseKernelDir(outRoot, release, device); err != nil {
		add("kernel-dir", "SKIP", err.Error())
	} else if ents, rerr := os.ReadDir(filepath.Join(outRoot, dir)); rerr != nil || len(ents) == 0 {
		add("kernel-dir", "FAIL", dir+" is missing or empty — the board reads RELEASE_KERNEL_<DEVICE>_DIR from it")
	} else {
		add("kernel-dir", "PASS", fmt.Sprintf("%s (%d entries)", dir, len(ents)))
	}

	// 4. Every Blueprint the seed wrote must still parse.
	var unparsable []string
	for _, root := range []string{filepath.Join("device", "google", family), filepath.Join("vendor", "google_devices", device)} {
		walkFiles(filepath.Join(outRoot, root), isBlueprint, func(p string) {
			b, err := os.ReadFile(p)
			if err != nil {
				return
			}
			if _, errs := parser.Parse(p, bytes.NewReader(b)); len(errs) > 0 {
				rel, _ := filepath.Rel(outRoot, p)
				unparsable = append(unparsable, filepath.ToSlash(rel))
			}
		})
	}
	sort.Strings(unparsable)
	if len(unparsable) > 0 {
		add("blueprints", "FAIL", fmt.Sprintf("%d Blueprint(s) do not parse: %s", len(unparsable), strings.Join(trim5(unparsable), " ")))
	} else {
		add("blueprints", "PASS", "every Blueprint under the device and vendor trees parses")
	}
	return cs
}

// linkTargetIsInsideBundle reports whether a bundle symlink points at something the bundle also
// carries, rather than out into the surrounding AOSP tree.
func linkTargetIsInsideBundle(l bundleSymlink) bool {
	target := pathClean(pathDir(l.Path) + "/" + l.Target)
	for _, top := range bundleTopDirs() {
		if target == top || strings.HasPrefix(target, top+"/") {
			return true
		}
	}
	return false
}

// pathDir and pathClean are the forward-slash forms filepath's OS-specific versions would mangle
// on a non-slash platform; bundle paths are always forward-slashed (io/fs requirement).
func pathDir(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return "."
}

func pathClean(p string) string {
	var out []string
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".":
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, seg)
		}
	}
	return strings.Join(out, "/")
}

// path0 is the first three segments of a bundle path ("device/google/lynx"), the unit a mirror
// either wrote or did not.
func path0(p string) string {
	f := strings.SplitN(p, "/", 4)
	if len(f) < 3 {
		return p
	}
	return f[0] + "/" + f[1] + "/" + f[2]
}

func trim5(xs []string) []string {
	if len(xs) <= 5 {
		return xs
	}
	return append(append([]string{}, xs[:5]...), fmt.Sprintf("(+%d more)", len(xs)-5))
}

// reportSeedVerification prints the checks and returns the number of failures.
func reportSeedVerification(device string, checks []seedCheck) int {
	fails := 0
	fmt.Printf("  verify-seed %s:\n", device)
	for _, c := range checks {
		fmt.Println("    " + c.String())
		if c.Status == "FAIL" {
			fails++
		}
	}
	return fails
}

func cmdVerifySeed(args []string) int {
	fs := flag.NewFlagSet("verify-seed", flag.ExitOnError)
	out := fs.String("out", "", "AOSP root the device was seeded into")
	devices := fs.String("devices", "", "comma-separated device names")
	release := fs.String("release", "", "release config the seed used (e.g. cp2a), to check the kernel prebuilt dir")
	_ = fs.Parse(args)
	if *out == "" || strings.TrimSpace(*devices) == "" {
		fmt.Fprintln(os.Stderr, "verify-seed: -out <aosp-root> and -devices <name,...> are required")
		return 2
	}
	total := 0
	for _, d := range strings.Split(*devices, ",") {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		total += reportSeedVerification(d, verifySeed(*out, d, "", *release))
	}
	if total == 0 {
		fmt.Println("VERDICT: the seeded tree satisfies every structural invariant the build depends on")
		return 0
	}
	fmt.Printf("VERDICT: %d FAIL — the seed is not complete; re-run `create -stock` after fixing, or report the failures\n", total)
	return 1
}
