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

func TestRequalifyLaneDeviceTextChangesPathsAndProductsOnly(t *testing.T) {
	in := []byte(`PRODUCT_NAME := aosp_cheetah_holo
$(call inherit-product, device/google/pantah-holo/device-cheetah_holo.mk)
PRODUCT_SOONG_NAMESPACES += frameworks-holo packages-holo
SOONG_CONFIG_holo_framework_routing += enable_holo_res
license: "device_google_pantah_holo_license"
PRODUCT_PACKAGES += HoloSettings
`)
	got := string(requalifyLaneDeviceText(in, "pantah", "holo", "holo2"))
	for _, want := range []string{
		"aosp_cheetah_holo2",
		"device/google/pantah-holo2/device-cheetah_holo2.mk",
		"frameworks-holo2 packages-holo2",
		"device_google_pantah_holo2_license",
		"SOONG_CONFIG_holo2_framework_routing",
		"enable_holo2_res",
		"HoloSettings",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("requalified device text missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "SOONG_CONFIG_holo_framework_routing") || strings.Contains(got, "Holo2Settings") {
		t.Fatalf("module/config identities were suffixed:\n%s", got)
	}
}

func TestCopyDeviceFamilyFromLanePreservesPayloadAndRenamesProductFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "device", "google", "pantah-holo")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "aosp_cheetah_holo.mk"), []byte("PRODUCT_NAME := aosp_cheetah_holo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "payload.pb"), []byte{0, 1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	c := deriveLane("holo2", false, []string{"cheetah"}, false, false, "")
	c.FromLane = "holo"
	copied, skipped, err := copyDeviceFamilyFromLane(c, root, "pantah")
	if err != nil || copied != 2 || skipped != 0 {
		t.Fatalf("copy = (%d, %d, %v), want (2, 0, nil)", copied, skipped, err)
	}
	b, err := os.ReadFile(filepath.Join(root, "device", "google", "pantah-holo2", "aosp_cheetah_holo2.mk"))
	if err != nil || string(b) != "PRODUCT_NAME := aosp_cheetah_holo2\n" {
		t.Fatalf("renamed product = %q, %v", b, err)
	}
	b, err = os.ReadFile(filepath.Join(root, "device", "google", "pantah-holo2", "payload.pb"))
	if err != nil || string(b) != string([]byte{0, 1, 2, 3}) {
		t.Fatalf("binary payload changed: %v, %v", b, err)
	}
}
