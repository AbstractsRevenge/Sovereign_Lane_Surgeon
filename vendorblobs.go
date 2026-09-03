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
		return "", fmt.Errorf("%q needs extracting from a filesystem image (%s.img) — not a flat file, not handled here", entry, category)
	}
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
		dst := filepath.Join(propDir, filepath.Base(entry))
		if _, statErr := os.Stat(dst); statErr == nil {
			wired++
			continue // no-clobber
		}
		src, lerr := locateFactoryBlob(factoryDir, entry)
		if lerr != nil {
			skipped++
			skipReasons = append(skipReasons, fmt.Sprintf("%s (%v)", entry, lerr))
			continue
		}
		if cerr := copyFile(src, dst); cerr != nil {
			return fmt.Errorf("copy %s: %w", entry, cerr)
		}
		fmt.Printf("  + vendor blob: %s\n", filepath.Base(dst))
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

	// Glue files: root/proprietary/{BoardConfigVendor.mk,device-vendor.mk} and
	// google_devices/staging/{BoardConfigPartial.mk,device-partial.mk} ALL land flat at
	// vendor/google_devices/<device>/ (confirmed from the real files' own -include/
	// inherit-product-if-exists targets — only the binary blobs go under proprietary/).
	glueSrcs := []string{
		filepath.Join(sxDir, "root", "proprietary", "BoardConfigVendor.mk"),
		filepath.Join(sxDir, "root", "proprietary", "device-vendor.mk"),
		filepath.Join(sxDir, "google_devices", "staging", "BoardConfigPartial.mk"),
		filepath.Join(sxDir, "google_devices", "staging", "device-partial.mk"),
	}
	for _, gs := range glueSrcs {
		if _, err := os.Stat(gs); err != nil {
			continue // optional — not every self-extractor ships every glue file
		}
		dst := filepath.Join(vendorDir, filepath.Base(gs))
		if _, statErr := os.Stat(dst); statErr == nil {
			continue // no-clobber
		}
		if cerr := copyFile(gs, dst); cerr != nil {
			return fmt.Errorf("copy glue file %s: %w", filepath.Base(gs), cerr)
		}
		fmt.Printf("  + vendor glue: %s\n", filepath.Base(gs))
	}
	// BoardConfigPartial.mk only exports android-info.txt as TARGET_BOARD_INFO_FILE under
	// USE_ANDROID_INFO (a Google-internal knob: `-include vendor/google/tools/android-info.mk`).
	// Set it in our copy of BoardConfigVendor.mk so the build's android-info.txt carries the
	// requirements above.
	return enableAndroidInfo(filepath.Join(vendorDir, "BoardConfigVendor.mk"))
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
