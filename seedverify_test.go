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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func statusOf(checks []seedCheck, name string) seedCheck {
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	return seedCheck{Name: name, Status: "ABSENT"}
}

// Make distinguishes an optional include from a required one, and so must the check: tegu names
// three vendor makefiles that exist only in an internal build, and their absence is intended.
func TestMkDirectivesClassifiesOptionalAndRequired(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "device-dev.mk"), strings.Join([]string{
		"$(call inherit-product-if-exists, vendor/google_devices/dev/prebuilts/device-vendor-dev.mk)",
		"$(call inherit-product, vendor/google_devices/dev/proprietary/device-vendor.mk)",
		"-include vendor/google_devices/dev/proprietary/BoardConfigVendor.mk",
		"include vendor/google_devices/dev/required.mk",
		"# include vendor/google_devices/dev/commented.mk",
		"include device/google/other/unrelated.mk",
	}, "\n"))
	req, opt := mkDirectives(root, "dev")
	wantReq := []string{"vendor/google_devices/dev/proprietary/device-vendor.mk", "vendor/google_devices/dev/required.mk"}
	wantOpt := []string{"vendor/google_devices/dev/prebuilts/device-vendor-dev.mk", "vendor/google_devices/dev/proprietary/BoardConfigVendor.mk"}
	if strings.Join(req, ",") != strings.Join(wantReq, ",") {
		t.Errorf("required:\n got %v\nwant %v", req, wantReq)
	}
	if strings.Join(opt, ",") != strings.Join(wantOpt, ",") {
		t.Errorf("optional:\n got %v\nwant %v", opt, wantOpt)
	}
}

// The regression that matters: glue written flat, where the device tree includes it from
// proprietary/. The file exists and every include is -if-exists, so only reachability finds it.
func TestOrphanVendorMakefileCatchesTheGlueBug(t *testing.T) {
	root := t.TempDir()
	famDir := filepath.Join(root, "device", "google", "fam")
	write(t, filepath.Join(famDir, "dev", "BoardConfig.mk"), "-include vendor/google_devices/dev/proprietary/BoardConfigVendor.mk\n")
	write(t, filepath.Join(famDir, "device-dev.mk"), "$(call inherit-product-if-exists, vendor/google_devices/dev/proprietary/device-vendor.mk)\n")
	vendorDir := filepath.Join(root, "vendor", "google_devices", "dev")
	// the bug: flat, not under proprietary/
	write(t, filepath.Join(vendorDir, "BoardConfigVendor.mk"), "-include vendor/google_devices/dev/BoardConfigPartial.mk\n")
	write(t, filepath.Join(vendorDir, "BoardConfigPartial.mk"), "BOARD_PREBUILT_VENDORIMAGE := x\n")
	write(t, filepath.Join(vendorDir, "proprietary", "device-vendor.mk"), "$(call inherit-product-if-exists, vendor/google_devices/dev/device-partial.mk)\n")
	write(t, filepath.Join(vendorDir, "device-partial.mk"), "PRODUCT_PACKAGES :=\n")
	// Android.mk is discovered by kati, never included — it must not be reported.
	write(t, filepath.Join(vendorDir, "proprietary", "Android.mk"), "LOCAL_PATH := $(call my-dir)\n")

	got := orphanVendorMakefiles(root, famDir, "dev")
	if len(got) != 1 || got[0] != "vendor/google_devices/dev/BoardConfigVendor.mk" {
		t.Fatalf("expected exactly the misplaced glue file, got %v", got)
	}
	// Put it where the tree includes it from: no orphans.
	if err := os.MkdirAll(filepath.Join(vendorDir, "proprietary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(vendorDir, "BoardConfigVendor.mk"), filepath.Join(vendorDir, "proprietary", "BoardConfigVendor.mk")); err != nil {
		t.Fatal(err)
	}
	if got := orphanVendorMakefiles(root, famDir, "dev"); len(got) != 0 {
		t.Fatalf("correctly placed glue reported as orphaned: %v", got)
	}
}

// End to end from the embedded bundle: a real seed into a temp root, with no external AOSP tree
// and no factory images, then the same verification create -stock runs on itself. This is the
// test that exercises mirroring, the reference closure and bundle fidelity together — the layer
// where every defect of this port actually lived.
func TestSeedFromBundleEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("copies ~hundreds of MB from the embedded bundle")
	}
	if !bundleAvailable() {
		t.Skip("built with -tags nobundle")
	}
	out := t.TempDir()
	if rc := cmdCreateStock(stockArgs{out: out, devices: "lynx", noVerify: true}); rc != 0 {
		t.Fatalf("create -stock returned %d", rc)
	}
	checks := verifySeed(out, "lynx", "", "")
	for _, name := range []string{"family-tree", "exec-bits", "symlinks", "blueprints"} {
		if c := statusOf(checks, name); c.Status != "PASS" {
			t.Errorf("%s: %s %s", name, c.Status, c.Detail)
		}
	}
	// No factory images were given, so there is no vendor tree to check.
	if c := statusOf(checks, "vendor-glue"); c.Status != "SKIP" {
		t.Errorf("vendor-glue without a vendor tree: %s %s", c.Status, c.Detail)
	}
	// The referenced-subtree closure must have pulled the SoC and shared trees in behind lynx.
	for _, rel := range []string{"device/google/lynx", "device/google/gs201", "device/google/gs-common", "hardware/google/pixel"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err != nil {
			t.Errorf("reference closure did not mirror %s: %v", rel, err)
		}
	}
	// And the properties embed drops must have survived into the real tree.
	link := filepath.Join(out, "hardware/google/graphics/zuma/include/displaycolor/displaycolor_gs101.h")
	if _, err := os.Stat(filepath.Join(out, "hardware/google/graphics/zuma")); err == nil {
		if _, err := os.Stat(link); err != nil {
			t.Errorf("symlink missing from a real seed: %v", err)
		}
	}
}

// The full pipeline including vendor blobs, which needs extracted factory images. Opt in by
// pointing SLS_TEST_FACTORY_ROOT at the parent of <device>/ — the same directory create -stock
// takes as -factory-images-root.
//
// No -release here: the kernel prebuilt directory is NAMED by the target tree's release config
// (build/release/flag_values/<rel>/RELEASE_KERNEL_<DEVICE>_DIR.textproto), which a bare temp root
// does not have, and refusing to guess it is correct behaviour. The checks this exercises are the
// ones that broke in practice: where the vendor glue landed, and where the blobs landed.
func TestSeedWithFactoryImagesEndToEnd(t *testing.T) {
	root := os.Getenv("SLS_TEST_FACTORY_ROOT")
	if root == "" {
		t.Skip("set SLS_TEST_FACTORY_ROOT to an extracted factory-image root to run the full seed")
	}
	device := os.Getenv("SLS_TEST_DEVICE")
	if device == "" {
		device = "cheetah"
	}
	if _, err := os.Stat(filepath.Join(root, device)); err != nil {
		t.Skipf("no factory images for %s under %s", device, root)
	}
	out := t.TempDir()
	if rc := cmdCreateStock(stockArgs{out: out, devices: device, factoryImagesRoot: root, noVerify: true}); rc != 0 {
		t.Fatalf("create -stock returned %d", rc)
	}
	checks := verifySeed(out, device, "", "")
	for _, c := range checks {
		if c.Status == "FAIL" {
			t.Errorf("%s", c)
		}
	}
	for _, name := range []string{"vendor-glue", "vendor-blobs", "exec-bits", "symlinks", "blueprints"} {
		if c := statusOf(checks, name); c.Status != "PASS" {
			t.Errorf("%s must PASS with factory images: %s %s", name, c.Status, c.Detail)
		}
	}
	// The glue must be exactly where the device tree includes it from, which is the regression.
	glue := filepath.Join(out, "vendor", "google_devices", device, "proprietary", "BoardConfigVendor.mk")
	if _, err := os.Stat(glue); err != nil {
		t.Errorf("vendor glue is not under proprietary/: %v", err)
	}
}
