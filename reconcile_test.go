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

func TestBundleSourceTags(t *testing.T) {
	if got := bundleSourceTag("device/google/tegu"); got != "android-15.0.0_r31" {
		t.Errorf("tegu tag %q", got)
	}
	if got := bundleSourceTag("device/google/pantah"); got != "android-15.0.0_r36" {
		t.Errorf("pantah tag %q", got)
	}
	if got := bundleSourceTag("device/google/nope"); got != "" {
		t.Errorf("unknown dir tag %q", got)
	}
}

func TestReconcileDropsIncludeOnlyWhenAbsent(t *testing.T) {
	entries := embeddedReconcileEntries()
	if len(entries) == 0 || entries[0].Makefile != "device/google/tegu/device-tegu.mk" {
		t.Fatalf("manifest: %+v", entries)
	}
	mkBody := "PRODUCT_PACKAGES += x\ninclude hardware/google/pixel/vibrator/cs40l26/device.mk\ninclude device/google/tegu/audio.mk\n"
	// target still has the included file → untouched
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"device/google/tegu/device-tegu.mk":                mkBody,
		"hardware/google/pixel/vibrator/cs40l26/device.mk": "# present\n",
	})
	if rep, err := applyReconciliations(root); err != nil || len(rep) != 0 {
		t.Fatalf("present include must be kept: %v %v", rep, err)
	}
	// target lacks it → the one line goes, nothing else
	root2 := t.TempDir()
	writeTree(t, root2, map[string]string{"device/google/tegu/device-tegu.mk": mkBody})
	rep, err := applyReconciliations(root2)
	if err != nil || len(rep) != 1 {
		t.Fatalf("rep %v err %v", rep, err)
	}
	b, _ := os.ReadFile(filepath.Join(root2, "device/google/tegu/device-tegu.mk"))
	if s := string(b); strings.Contains(s, "cs40l26") || !strings.Contains(s, "include device/google/tegu/audio.mk") || !strings.Contains(s, "PRODUCT_PACKAGES += x") {
		t.Fatalf("bad edit:\n%s", s)
	}
	if rep2, _ := applyReconciliations(root2); len(rep2) != 0 {
		t.Fatal("not idempotent")
	}
}
