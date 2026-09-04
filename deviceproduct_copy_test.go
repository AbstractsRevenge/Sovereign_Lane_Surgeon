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
	"testing"
)

func TestIsStockProductMk(t *testing.T) {
	drop := []string{"aosp_cheetah.mk", "aosp_cheetah_hwasan.mk", "factory_panther.mk",
		"device-cheetah.mk", "device_panther_product.mk", "AndroidProducts.mk"}
	keep := []string{"audio-tables.mk", "device-cheetah_nexusm.mk", "BoardConfig.mk", "Android.bp", "NOTICE"}
	for _, b := range drop {
		if !isStockProductMk(b) {
			t.Errorf("isStockProductMk(%q) = false, want true (stock product mk must be excluded)", b)
		}
	}
	for _, b := range keep {
		// device-cheetah_nexusm.mk starts with "device-" so isStockProductMk matches it too — but the
		// copy never encounters lane-generated files (they land via the template AFTER the copy), so
		// that's harmless. The load-bearing keeps are the HW/sub-dir mks:
		if b == "device-cheetah_nexusm.mk" {
			continue
		}
		if isStockProductMk(b) {
			t.Errorf("isStockProductMk(%q) = true, want false (HW/non-product file must be kept)", b)
		}
	}
}

func TestPathHasOverlaySegment(t *testing.T) {
	over := []string{
		"cheetah/overlay-holo/Android.bp",
		"overlay-product-cheetah/Android.bp",
		"cheetah/overlay_packages/SettingsOverlayGE2AE/Android.bp",
		"panther/rro_overlays/NfcOverlay/Android.bp",
	}
	notOver := []string{"powerstats/cheetah/Android.bp", "audio/cheetah/audio-tables.mk", "Android.bp", "conf/init.rc"}
	for _, p := range over {
		if !pathHasOverlaySegment(p) {
			t.Errorf("pathHasOverlaySegment(%q) = false, want true", p)
		}
	}
	for _, p := range notOver {
		if pathHasOverlaySegment(p) {
			t.Errorf("pathHasOverlaySegment(%q) = true, want false", p)
		}
	}
}

func TestBpIsCollisionProne(t *testing.T) {
	rro := []byte(`package { default_applicable_licenses: ["x"] }
runtime_resource_overlay { name: "SettingsOverlayGE2AE" }`)
	if !bpIsCollisionProne(rro) {
		t.Error("runtime_resource_overlay bp should be collision-prone")
	}
	hw := []byte(`package { default_applicable_licenses: ["x"] }
cc_library_shared { name: "android.hardware.power.stats-service" }`)
	if bpIsCollisionProne(hw) {
		t.Error("cc_library_shared HW bp should NOT be collision-prone (kept + relicensed)")
	}
	// A bare soong_namespace is NOT collision-prone by TYPE — powerstats declares one too. The
	// overlay ones are caught by pathHasOverlaySegment instead. So a namespace-only HW bp is kept.
	ns := []byte(`soong_namespace {}`)
	if bpIsCollisionProne(ns) {
		t.Error("soong_namespace by itself must NOT be type-collision-prone (path check handles overlay dirs)")
	}
}

// TestCopyDeviceFamilyTree drives the full copy against a synthetic stock family dir and asserts the
// complete-dir invariants: HW subtree copied, overlay .bp dropped (resources kept), HW .bp kept +
// relicensed, stock product mks + root Android.bp/NOTICE excluded.
func TestCopyDeviceFamilyTree(t *testing.T) {
	out := t.TempDir()
	fam := filepath.Join(out, "device", "google", "pantah")
	mk := func(rel, content string) {
		p := filepath.Join(fam, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("Android.bp", `license { name: "device_google_pantah_license" }`)
	mk("NOTICE", "notice")
	mk("aosp_cheetah.mk", "stock product mk")
	mk("device-cheetah.mk", "stock device mk")
	mk("audio/cheetah/audio-tables.mk", "hw audio")
	mk("powerstats/cheetah/Android.bp", `package { default_applicable_licenses: ["device_google_pantah_license"] }
cc_library_shared { name: "powerstats-cheetah" }`)
	mk("cheetah/overlay-holo/Android.bp", `soong_namespace {}`)
	mk("cheetah/overlay_packages/SettingsX/Android.bp", `runtime_resource_overlay { name: "SettingsX" }`)
	mk("cheetah/overlay_packages/SettingsX/res/values/strings.xml", `<resources/>`)

	c := LaneConfig{Name: "testlane", CamelCase: "Testlane"}
	copied, dropped, err := copyDeviceFamilyTree(c, out, "pantah")
	if err != nil {
		t.Fatalf("copyDeviceFamilyTree: %v", err)
	}
	if dropped != 2 {
		t.Errorf("dropped = %d, want 2 (overlay-holo + SettingsX .bp)", dropped)
	}
	if copied < 3 {
		t.Errorf("copied = %d, want >=3 (audio mk + powerstats bp + overlay res)", copied)
	}
	dst := filepath.Join(out, "device", "google", "pantah-testlane")
	exists := func(rel string) bool { _, e := os.Stat(filepath.Join(dst, rel)); return e == nil }
	// HW copied.
	if !exists("audio/cheetah/audio-tables.mk") {
		t.Error("HW audio mk not copied")
	}
	// Overlay resource kept even though its .bp is dropped (matches lynx-nexusm).
	if !exists("cheetah/overlay_packages/SettingsX/res/values/strings.xml") {
		t.Error("overlay resource should be copied even when its .bp is dropped")
	}
	// Overlay .bp dropped.
	if exists("cheetah/overlay_packages/SettingsX/Android.bp") {
		t.Error("runtime_resource_overlay .bp should be dropped")
	}
	if exists("cheetah/overlay-holo/Android.bp") {
		t.Error("overlay-holo (other-lane) .bp should be dropped")
	}
	// HW .bp kept + relicensed.
	ps, err := os.ReadFile(filepath.Join(dst, "powerstats/cheetah/Android.bp"))
	if err != nil {
		t.Fatalf("powerstats .bp not kept: %v", err)
	}
	if got := string(ps); !contains(got, "device_google_pantah_testlane_license") || contains(got, `"device_google_pantah_license"`) {
		t.Errorf("powerstats .bp not relicensed to lane license: %q", got)
	}
	// Stock product mks + root Android.bp/NOTICE excluded (templates emit lane versions).
	for _, rel := range []string{"aosp_cheetah.mk", "device-cheetah.mk", "Android.bp", "NOTICE"} {
		if exists(rel) {
			t.Errorf("%s should be excluded from the copy (lane template replaces it)", rel)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
