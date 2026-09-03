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
	"os"
	"path/filepath"
	"strings"
)

// vendorblobs.go — wires a device's real vendor-image blobs (sourced from an already-extracted
// factory image) into vendor/google_devices/<device>/, following the exact mechanism the stock
// tree already ships as device/google/<family>/self-extractors_<device>/ (multi-product family,
// e.g. pantah's self-extractors_panther) or device/google/<family>/self-extractors/
// (single-product family, e.g. lynx/tangorpro) — Google's own source for the "Vendor image,
// Binaries for AOSP" self-extracting installer. Populating vendor/google_devices/ directly from an
// already-extracted factory image is equivalent to running that installer. NOTE: only the
// IMAGES/RADIO-category entries (flat files) are handled here — system_ext-category entries (e.g.
// Shannon IMS/RCS files) live inside system_ext.img, a filesystem image that needs mounting/
// extraction (loop mount needs privileges, or an ext4/erofs userspace reader); those are reported,
// not silently skipped, so a caller knows real work remains rather than assuming completeness.

// findSelfExtractorDir locates a device's self-extractor source dir under outRoot, trying the
// multi-product family naming (self-extractors_<device>) before the single-product naming
// (self-extractors) — both are real, confirmed against stock AOSP (pantah uses the former,
// lynx/tangorpro the latter).
func findSelfExtractorDir(outRoot, family, device string) (string, error) {
	suffixed := filepath.Join(outRoot, "device", "google", family, "self-extractors_"+device)
	if info, err := os.Stat(suffixed); err == nil && info.IsDir() {
		return suffixed, nil
	}
	plain := filepath.Join(outRoot, "device", "google", family, "self-extractors")
	if info, err := os.Stat(plain); err == nil && info.IsDir() {
		return plain, nil
	}
	return "", fmt.Errorf("no self-extractors dir found for %s under device/google/%s", device, family)
}

// parseExtractList reads extract-lists.txt and returns the TO_EXTRACT file list from the given
// shell case label's block (e.g. "google_devices") — a plain line-scan of a shell data file, not
// Go/Blueprint source (HARD RULE 3 governs AST-editable source, not reading a shell fragment).
// Format (confirmed against the real file):
//
//	google_devices)
//	  TO_EXTRACT="\
//	          IMAGES/vendor.img \
//	          RADIO/bootloader.img \
//	          "
//	  ;;
func parseExtractList(path, caseLabel string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	inCase, inList := false, false
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case !inCase:
			if line == caseLabel+")" {
				inCase = true
			}
		case line == ";;":
			return out, nil
		default:
			if strings.HasPrefix(line, "TO_EXTRACT=") {
				inList = true
				line = strings.TrimPrefix(line, `TO_EXTRACT="`)
			}
			if !inList {
				continue
			}
			entry := strings.TrimSuffix(line, `\`)
			entry = strings.TrimSuffix(strings.TrimSpace(entry), `"`)
			entry = strings.TrimSpace(entry)
			if entry != "" {
				out = append(out, entry)
			}
		}
	}
	return nil, fmt.Errorf("case block %q not closed (missing \";;\") in %s", caseLabel, path)
}

// locateFactoryBlob finds the actual file in a factory-image extraction directory that
// corresponds to an extract-lists.txt entry like "IMAGES/vendor.img" or "RADIO/bootloader.img".
// Confirmed against a real extraction (not assumed): IMAGES-category partitions sit FLAT at the
// extraction root with their generic name (vendor.img, vendor_dlkm.img, vbmeta_vendor.img) —
// exact-name match. RADIO-category partitions nest one level down under a <device>-<buildid>/
// subdirectory with a versioned/branded filename (bootloader-panther-cloudripper-17.0-XXXX.img,
// not "bootloader.img") — matched by base-name PREFIX, not exact name. Any other category (e.g.
// system_ext) is NOT handled here — see the file-level doc comment.
func locateFactoryBlob(factoryDir, entry string) (string, error) {
	return locateBlob(factoryDir, "", entry)
}

// locateBlob is locateFactoryBlob plus the filesystem-image categories: an entry like
// "system_ext/priv-app/ShannonIms/ShannonIms.apk" names a file INSIDE system_ext.img, which
// extractVendorImages unpacks to <vendorDir>/system_ext/ first (no root, debugfs) — the file is
// then looked up there by its exact path. Google's installer reads the same file out of the
// same image; only the unpacking step is ours.
func locateBlob(factoryDir, vendorDir, entry string) (string, error) {
	parts := strings.SplitN(entry, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("unrecognized extract-lists.txt entry %q", entry)
	}
	category, rest := parts[0], parts[1]
	switch category {
	case "IMAGES":
		p := filepath.Join(factoryDir, rest)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("%s not found flat under %s", rest, factoryDir)
	case "RADIO":
		base := strings.TrimSuffix(rest, filepath.Ext(rest)) // "bootloader" from "bootloader.img"
		var found string
		_ = filepath.Walk(factoryDir, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() || found != "" {
				return nil
			}
			if strings.HasPrefix(filepath.Base(p), base) {
				found = p
			}
			return nil
		})
		if found == "" {
			return "", fmt.Errorf("no file starting with %q found under %s", base, factoryDir)
		}
		return found, nil
	default:
		if vendorDir != "" {
			p := filepath.Join(vendorDir, category, filepath.FromSlash(rest))
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
		return "", fmt.Errorf("%q lives inside %s.img — extract it first (extract-vendor / create -stock unpack it to vendor/google_devices/<device>/%s/)", entry, category, category)
	}
}

// blobDestination derives where a wired blob goes under proprietary/ from the files that
// CONSUME it: the self-extractor's staging device-partial.mk (PRODUCT_COPY_FILES source paths),
// Android.bp.txt (prebuilt srcs such as "lib64/libmediaadaptor.so") and Android.mk.template
// (LOCAL_SRC_FILES). The first path ending in "/<base>" wins; a blob nobody names by path lands
// flat, which is what Google's installer does (unzip -j). Checked against the real cheetah
// staging files: xml and apk flat, libmediaadaptor.so under lib64/.
func blobDestination(sxDir, device, base string) string {
	staging := filepath.Join(sxDir, "google_devices", "staging")
	prefix := "vendor/google_devices/" + device + "/proprietary/"
	for _, f := range []string{"device-partial.mk", "Android.bp.txt", "Android.mk.template"} {
		b, err := os.ReadFile(filepath.Join(staging, f))
		if err != nil {
			continue
		}
		for _, tok := range strings.FieldsFunc(string(b), func(r rune) bool {
			return r == ' ' || r == '\t' || r == '"' || r == ':' || r == ',' || r == '\n' || r == '\\' || r == '[' || r == ']'
		}) {
			if !strings.HasSuffix(tok, "/"+base) && tok != base {
				continue
			}
			rel := strings.TrimPrefix(tok, prefix)
			if strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") || strings.HasPrefix(rel, "$") {
				continue
			}
			if rel == base {
				return base
			}
			if strings.Contains(rel, "/") && !strings.Contains(rel, "vendor/google_devices") {
				return rel
			}
		}
	}
	return base
}

// glueDestination derives where a self-extractor glue file (BoardConfigVendor.mk, device-vendor.mk,
// BoardConfigPartial.mk, device-partial.mk) must land from the file that INCLUDES it: the device
// family tree's own `-include vendor/google_devices/<device>/…/<base>` and
// `inherit-product-if-exists` lines, and the glue files' includes of each other. Every cp2a family
// in android-17 (checked 2026-09-03, 16 devices) includes BoardConfigVendor.mk and device-vendor.mk
// from proprietary/ and those two include BoardConfigPartial.mk / device-partial.mk flat. Returns
// the path relative to vendor/google_devices/<device>/; falls back to the self-extractor's own
// layout (root/ maps onto that directory, staging/ onto its root) when no include names the file.
func glueDestination(outRoot, family, device, sxSrc string) string {
	base := filepath.Base(sxSrc)
	needle := "vendor/google_devices/" + device + "/"
	found := ""
	scan := func(p string) {
		if found != "" {
			return
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return
		}
		for _, tok := range strings.Fields(string(b)) {
			tok = strings.Trim(tok, "(),")
			if strings.HasPrefix(tok, needle) && strings.HasSuffix(tok, "/"+base) {
				found = strings.TrimPrefix(tok, needle)
				return
			}
		}
	}
	walkFiles(filepath.Join(outRoot, "device", "google", family), func(n string) bool { return strings.HasSuffix(n, ".mk") }, scan)
	if found != "" {
		return filepath.FromSlash(found)
	}
	if rel, err := filepath.Rel(filepath.Join(filepath.Dir(filepath.Dir(sxSrc))), sxSrc); err == nil && strings.HasPrefix(filepath.ToSlash(rel), "proprietary/") {
		return rel // root/proprietary/<base> → proprietary/<base>
	}
	return base
}

// wireVendorBlobs populates vendor/google_devices/<device>/ from an already-extracted factory
// image, following the real self-extractors_<device>/ (or self-extractors/) mechanism the stock
// tree ships. Equivalent to running Google's self-extracting "Vendor image, Binaries for AOSP"
// installer, sourced from factoryDir instead. No-clobber throughout.
func wireVendorBlobs(outRoot, family, device, factoryDir string) error {
	sxDir, err := findSelfExtractorDir(outRoot, family, device)
	if err != nil {
		return err
	}
	entries, err := parseExtractList(filepath.Join(sxDir, "extract-lists.txt"), "google_devices")
	if err != nil {
		return err
	}
	vendorDir := filepath.Join(outRoot, "vendor", "google_devices", device)
	propDir := filepath.Join(vendorDir, "proprietary")
	if err := os.MkdirAll(propDir, 0o755); err != nil {
		return err
	}

	var wired, skipped int
	var skipReasons []string
	for _, entry := range entries {
		dst := filepath.Join(propDir, blobDestination(sxDir, device, filepath.Base(entry)))
		if _, statErr := os.Stat(dst); statErr == nil {
			wired++
			continue // no-clobber
		}
		src, lerr := locateBlob(factoryDir, vendorDir, entry)
		if lerr != nil {
			skipped++
			skipReasons = append(skipReasons, fmt.Sprintf("%s (%v)", entry, lerr))
			continue
		}
		if merr := os.MkdirAll(filepath.Dir(dst), 0o755); merr != nil {
			return merr
		}
		if cerr := copyFile(src, dst); cerr != nil {
			return fmt.Errorf("copy %s: %w", entry, cerr)
		}
		rel, _ := filepath.Rel(propDir, dst)
		fmt.Printf("  + vendor blob: %s\n", filepath.ToSlash(rel))
		wired++
	}
	if len(skipReasons) > 0 {
		fmt.Printf("  ! %d/%d blob(s) not resolved (likely inside a filesystem image needing separate extraction):\n", skipped, len(entries))
		for _, s := range skipReasons {
			fmt.Printf("      %s\n", s)
		}
	}

	// android-info.txt comes from the FACTORY image, not the self-extractor: the self-extractor's
	// copy is whatever AOSP 15 shipped (cheetah: "require version-bootloader=cloudripper-1.0-8428895",
	// a 2022 bootloader) while the factory image's names the bootloader and baseband the blobs
	// being wired were built against (CP2A: cloudripper-17.0-15199429, g5300q-260317-260505). A
	// vendor built for one firmware does not boot on another — observed 2026-09-03 on cheetah:
	// factory-content super, bootloader 15.2, userspace died and rebooted to the bootloader at ~65 s
	// with no log, until the July firmware was flashed. fastboot's flashall checks exactly these
	// lines, so carrying the real ones makes it refuse the mismatch up front.
	if src := filepath.Join(factoryDir, "android-info.txt"); fileExists(src) {
		dst := filepath.Join(vendorDir, "android-info.txt")
		cur, _ := os.ReadFile(dst)
		stale, _ := os.ReadFile(filepath.Join(sxDir, "root", "android-info.txt"))
		// Write when absent, or replace a copy that is still the self-extractor's template
		// (a tree revived before this rule); a hand-edited file is left alone.
		if cur == nil || (stale != nil && string(cur) == string(stale)) {
			if cerr := copyFile(src, dst); cerr != nil {
				return fmt.Errorf("copy android-info.txt: %w", cerr)
			}
			fmt.Printf("  + vendor glue: android-info.txt (from the factory image: firmware requirements of these blobs)\n")
		}
	}

	// Glue files. WHERE each lands is read from the tree that includes it (glueDestination), not
	// assumed: device/google/<family>/<device>/BoardConfig.mk says
	// `-include vendor/google_devices/<device>/proprietary/BoardConfigVendor.mk` and
	// device-<device>.mk inherits `…/proprietary/device-vendor.mk`; those two then include
	// BoardConfigPartial.mk / device-partial.mk FLAT. An earlier revision of this function put all
	// four flat (a wrong reading of "their own include targets"), and the consequence was exact
	// and silent (cheetah, 2026-09-02, run 152554Z): the build never saw BOARD_PREBUILT_VENDORIMAGE,
	// so Make's filter-out-missing-vendor dropped vendor/vendor_dlkm from super.img, no
	// vbmeta_vendor was built, android-info.txt carried no firmware requirement, and
	// device-partial.mk's packages never installed. assemble-super papered over the first of
	// those. Flat copies from that revision are moved to the derived location (idempotent).
	// LICENSE and COPYRIGHT ship at the package root: BoardConfigPartial.mk's VENDOR_BLOBS_LICENSE
	// names vendor/google_devices/<device>/LICENSE (android-17's fsgen makes a license module of it
	// and fails `m nothing` when the file is absent — observed once the glue was included, run
	// 032204Z) and proprietary/Android.mk's LOCAL_NOTICE_FILE names both.
	glueSrcs := []string{
		filepath.Join(sxDir, "root", "proprietary", "BoardConfigVendor.mk"),
		filepath.Join(sxDir, "root", "proprietary", "device-vendor.mk"),
		filepath.Join(sxDir, "google_devices", "staging", "BoardConfigPartial.mk"),
		filepath.Join(sxDir, "google_devices", "staging", "device-partial.mk"),
		filepath.Join(sxDir, "google_devices", "LICENSE"),
		filepath.Join(sxDir, "google_devices", "COPYRIGHT"),
	}
	boardConfigVendor := filepath.Join(vendorDir, "BoardConfigVendor.mk")
	for _, gs := range glueSrcs {
		if _, err := os.Stat(gs); err != nil {
			continue // optional — not every self-extractor ships every glue file
		}
		rel := glueDestination(outRoot, family, device, gs)
		dst := filepath.Join(vendorDir, rel)
		if filepath.Base(gs) == "BoardConfigVendor.mk" {
			boardConfigVendor = dst
		}
		if flat := filepath.Join(vendorDir, filepath.Base(gs)); flat != dst && fileExists(flat) && !fileExists(dst) {
			if merr := os.MkdirAll(filepath.Dir(dst), 0o755); merr != nil {
				return merr
			}
			if rerr := os.Rename(flat, dst); rerr != nil {
				return fmt.Errorf("move glue file %s: %w", filepath.Base(gs), rerr)
			}
			fmt.Printf("  + vendor glue: %s moved to %s (where device/google/%s includes it from)\n", filepath.Base(gs), filepath.ToSlash(rel), family)
			continue
		}
		if fileExists(dst) {
			continue // no-clobber
		}
		if merr := os.MkdirAll(filepath.Dir(dst), 0o755); merr != nil {
			return merr
		}
		if cerr := copyFile(gs, dst); cerr != nil {
			return fmt.Errorf("copy glue file %s: %w", filepath.Base(gs), cerr)
		}
		fmt.Printf("  + vendor glue: %s\n", filepath.ToSlash(rel))
	}
	// The staging module definitions: Android.mk.template (radio files + the PRESIGNED system_ext
	// apks device-partial.mk lists in PRODUCT_PACKAGES) and Android.bp.txt (the prebuilt
	// libraries, in the soong namespace device-partial.mk registers). Google's installer writes
	// them as proprietary/Android.mk and proprietary/Android.bp; the template carries no
	// placeholders (checked: cheetah's names the device literally).
	for src, name := range map[string]string{"Android.mk.template": "Android.mk", "Android.bp.txt": "Android.bp"} {
		sp := filepath.Join(sxDir, "google_devices", "staging", src)
		dst := filepath.Join(propDir, name)
		if !fileExists(sp) || fileExists(dst) {
			continue
		}
		if cerr := copyFile(sp, dst); cerr != nil {
			return fmt.Errorf("copy %s: %w", src, cerr)
		}
		fmt.Printf("  + vendor glue: proprietary/%s (from staging/%s)\n", name, src)
	}
	// BoardConfigPartial.mk only exports android-info.txt as TARGET_BOARD_INFO_FILE under
	// USE_ANDROID_INFO (a Google-internal knob: `-include vendor/google/tools/android-info.mk`).
	// Set it in our copy of BoardConfigVendor.mk so the build's android-info.txt carries the
	// requirements above.
	return enableAndroidInfo(boardConfigVendor)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// enableAndroidInfo prepends `USE_ANDROID_INFO := true` to the vendor BoardConfigVendor.mk when
// it does not set it yet. Idempotent; a missing file is left alone.
func enableAndroidInfo(boardConfigVendor string) error {
	b, err := os.ReadFile(boardConfigVendor)
	if err != nil {
		return nil
	}
	if strings.Contains(string(b), "USE_ANDROID_INFO") {
		return nil
	}
	head := "# The build's android-info.txt must carry the bootloader/baseband the wired vendor blobs\n" +
		"# require (see vendor/google_devices/<device>/android-info.txt, taken from the factory image);\n" +
		"# BoardConfigPartial.mk exports it only under USE_ANDROID_INFO. Set by sovereign-lane-surgeon.\n" +
		"USE_ANDROID_INFO := true\n\n"
	if err := os.WriteFile(boardConfigVendor, append([]byte(head), b...), 0o644); err != nil {
		return err
	}
	fmt.Printf("  + vendor glue: USE_ANDROID_INFO := true in %s\n", filepath.Base(boardConfigVendor))
	return nil
}
