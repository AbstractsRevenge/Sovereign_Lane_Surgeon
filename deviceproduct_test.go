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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lynxRes is the resolved lynx device (family==product==lynx, gs201).
func lynxRes() deviceResolution {
	return deviceResolution{Product: "lynx", ProductTitle: "Lynx", Family: "lynx", SoC: "gs201", Resolved: true}
}

// TestGenDeviceProduct checks the holo/lynx render reproduces the LANE-UNIVERSAL lines of the
// proven aosp_lynx_holo.mk, auto-fills the SoC inherit, and keeps other blocks TODO.
func TestGenDeviceProduct(t *testing.T) {
	cfg := deriveLane("holo", true, []string{"lynx"}, true, true, "")
	rel, content, err := genDeviceProduct(cfg, lynxRes(), deviceProductTmpl, "aosp_lynx_holo.mk")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if rel != "device/google/lynx-holo/aosp_lynx_holo.mk" {
		t.Errorf("path = %q", rel)
	}
	universal := []string{
		"PRODUCT_DEFAULT_DEV_CERTIFICATE := build/make/target/product/security/holo/holo_platform",
		"SOONG_CONFIG_holo_framework_routing_enable_holo_kotlinc := true",
		"    system/priv-app/Holo%",
		"PRODUCT_NAME := aosp_lynx_holo",
		"PRODUCT_MODEL := AOSP Holo on Lynx",
		"PRODUCT_DEVICE := lynx",
		"DEVICE_MANIFEST_FILE := device/google/lynx-holo/manifest.xml",
		"ro.holo.framework=true",
		// Native curated audio (NOT a wildcard HoloAudio.mk bolt-on — that ships a contaminated superset).
		"PRODUCT_PACKAGES += frameworks_sounds",
		// Corrected 4.3 default (was the non-4.3 Aldebaran.ogg) — guards the contamination from regressing.
		"ro.config.notification_sound=Tejat.ogg",
		// SoC auto-filled + device-HW inherit use family=lynx — both match real lynx-holo.
		"$(call inherit-product, device/google/gs201/aosp_common.mk)",
		"$(call inherit-product, device/google/lynx-holo/device-lynx_holo.mk)",
	}
	for _, u := range universal {
		if !strings.Contains(content, u) {
			t.Errorf("missing universal line: %q", u)
		}
	}
	for _, todo := range []string{"TODO(lynx): device/SoC hardware knobs", "TODO(holo): the lane app surface"} {
		if !strings.Contains(content, todo) {
			t.Errorf("missing TODO slot: %q", todo)
		}
	}
	if strings.Contains(content, "Pattern template (per NexusM precedent)") {
		t.Error("templated the truncated stub — must not")
	}
}

// TestGenDeviceProductFamily is the key case T flagged: cheetah is a PRODUCT in the pantah FAMILY.
// The device dir must be pantah-<lane>, the product aosp_cheetah_<lane>, SoC gs201.
func TestGenDeviceProductFamily(t *testing.T) {
	cfg := deriveLane("testing", true, []string{"cheetah"}, true, true, "")
	res := deviceResolution{Product: "cheetah", ProductTitle: "Cheetah", Family: "pantah", SoC: "gs201", Resolved: true}
	rel, content, err := genDeviceProduct(cfg, res, deviceProductTmpl, "aosp_cheetah_testing.mk")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "device/google/pantah-testing/aosp_cheetah_testing.mk" {
		t.Errorf("path = %q, want device/google/pantah-testing/... (family dir, NOT cheetah-testing)", rel)
	}
	for _, want := range []string{
		"PRODUCT_NAME := aosp_cheetah_testing",
		"PRODUCT_DEVICE := cheetah",
		"$(call inherit-product, device/google/gs201/aosp_common.mk)",
		"$(call inherit-product, device/google/pantah-testing/device-cheetah_testing.mk)",
		"DEVICE_MANIFEST_FILE := device/google/pantah-testing/manifest.xml",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("cheetah/pantah render missing %q", want)
		}
	}
	// Must NOT create a cheetah-testing device dir anywhere.
	if strings.Contains(content, "device/google/cheetah-testing/") {
		t.Error("leaked cheetah-testing as a device dir — family is pantah")
	}
}

// TestGenDeviceProductParam confirms parameterization + unresolved-SoC TODO on a fresh lane.
func TestGenDeviceProductParam(t *testing.T) {
	cfg := deriveLane("aurora", true, []string{"pixel9"}, true, true, "")
	res := deviceResolution{Product: "pixel9", ProductTitle: "Pixel9", Family: "pixel9"} // unresolved: SoC ""
	_, content, err := genDeviceProduct(cfg, res, deviceProductTmpl, "aosp_pixel9_aurora.mk")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"PRODUCT_NAME := aosp_pixel9_aurora",
		"PRODUCT_MODEL := AOSP Aurora on Pixel9",
		"    system/priv-app/Aurora%",
		"ro.aurora.framework=true",
		"TODO(pixel9): inherit the SoC-common product", // unresolved SoC → TODO
	} {
		if !strings.Contains(content, want) {
			t.Errorf("missing parameterized line: %q", want)
		}
	}
	for _, bad := range []string{"holo", "lynx", "Holo", "Lynx"} {
		if strings.Contains(content, bad) {
			t.Errorf("leaked source-lane token %q", bad)
		}
	}
}

// TestResolveDevice derives family + SoC from a fake tree (cheetah under pantah, gs201).
func TestResolveDevice(t *testing.T) {
	root := t.TempDir()
	pantah := filepath.Join(root, "device", "google", "pantah")
	if err := os.MkdirAll(pantah, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(pantah, "AndroidProducts.mk"),
		[]byte("PRODUCT_MAKEFILES := \\\n    $(LOCAL_DIR)/aosp_cheetah.mk\n"), 0o644)
	os.WriteFile(filepath.Join(pantah, "aosp_cheetah.mk"),
		[]byte("$(call inherit-product, device/google/gs201/aosp_common.mk)\nPRODUCT_DEVICE := cheetah\n"), 0o644)

	res := resolveDevice(root, "cheetah")
	if !res.Resolved {
		t.Fatal("cheetah should resolve")
	}
	if res.Family != "pantah" {
		t.Errorf("family = %q, want pantah", res.Family)
	}
	if res.SoC != "gs201" {
		t.Errorf("soc = %q, want gs201", res.SoC)
	}
	// Unknown product falls back gracefully.
	un := resolveDevice(root, "nosuchdevice")
	if un.Resolved || un.Family != "nosuchdevice" {
		t.Errorf("unknown device should fall back to family==product, got %+v", un)
	}
}

// TestGenDeviceCompanions checks the AndroidProducts.mk / Android.bp / device-mk stubs.
func TestGenDeviceCompanions(t *testing.T) {
	cfg := deriveLane("holo", true, []string{"lynx"}, true, true, "")

	_, apm, err := genDeviceProduct(cfg, lynxRes(), androidProductsMkTmpl, "AndroidProducts.mk")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{
		"    $(LOCAL_DIR)/aosp_lynx_holo.mk",
		"    aosp_lynx_holo-bp1a-userdebug \\",
		"    aosp_lynx_holo-bp1a-eng",
	} {
		if !strings.Contains(apm, w) {
			t.Errorf("AndroidProducts.mk missing %q", w)
		}
	}

	_, bp, err := genDeviceProduct(cfg, lynxRes(), deviceAndroidBpTmpl, "Android.bp")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bp, `name: "device_google_lynx_holo_license"`) {
		t.Error("Android.bp missing license module name")
	}

	_, dmk, err := genDeviceProduct(cfg, lynxRes(), deviceMkStubTmpl, "device-lynx_holo.mk")
	if err != nil {
		t.Fatal(err)
	}
	// Resolved device → inherits the STOCK HW body (device/google/<family>/device-<product>.mk).
	if !strings.Contains(dmk, "$(call inherit-product, device/google/lynx/device-lynx.mk)") {
		t.Errorf("device-lynx_holo.mk missing stock-HW inherit:\n%s", dmk)
	}
	// Unresolved device → the stock-HW inherit is a TODO (commented), not a live inherit.
	unRes := deviceResolution{Product: "pixel9", ProductTitle: "Pixel9", Family: "pixel9"}
	_, dmk2, _ := genDeviceProduct(cfg, unRes, deviceMkStubTmpl, "device-pixel9_holo.mk")
	if !strings.Contains(dmk2, "TODO(pixel9): inherit the stock device HW body") {
		t.Error("unresolved device should TODO the stock-HW inherit")
	}
	if strings.Contains(dmk2, "\n$(call inherit-product, device/google/pixel9/device-pixel9.mk)") {
		t.Error("unresolved device must not emit a LIVE (uncommented) stock-HW inherit")
	}
}

// board_config.mk finds the board by GLOB (`*/$(TARGET_DEVICE)/BoardConfig.mk`) and the lane keeps
// the stock PRODUCT_DEVICE, so a copied board subdir matches for every product of that device and
// kati aborts with "Multiple board config files". The lane shares the stock board.
func TestCopyDeviceFamilyTreeSkipsBoardDirs(t *testing.T) {
	root := t.TempDir()
	fam := filepath.Join(root, "device", "google", "pantah")
	for _, d := range []string{"panther", "cheetah", "audio"} {
		if err := os.MkdirAll(filepath.Join(fam, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// board subdirs are the ones carrying BoardConfig.mk
	for _, d := range []string{"panther", "cheetah"} {
		if err := os.WriteFile(filepath.Join(fam, d, "BoardConfig.mk"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(fam, "audio", "mixer.xml"), []byte("<x/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := deriveLane("zed", true, nil, false, false, "")
	if _, _, err := copyDeviceFamilyTree(c, root, "pantah"); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "device", "google", "pantah-zed")
	for _, d := range []string{"panther", "cheetah"} {
		if _, err := os.Stat(filepath.Join(dst, d, "BoardConfig.mk")); err == nil {
			t.Errorf("board subdir %q must NOT be copied (kati globs it for every %s product)", d, d)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "audio", "mixer.xml")); err != nil {
		t.Error("non-board HW subdirs must still be copied")
	}
}
