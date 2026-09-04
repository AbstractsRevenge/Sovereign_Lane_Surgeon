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

func TestNeutralizedByBak(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "live.mk")
	neutralized := filepath.Join(dir, "neutralized.mk")
	if err := os.WriteFile(neutralized+".bak", []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !neutralizedByBak(neutralized) {
		t.Errorf("neutralizedByBak(%q) = false, want true (a %q.bak sibling exists)", neutralized, neutralized)
	}
	if neutralizedByBak(live) {
		t.Errorf("neutralizedByBak(%q) = true, want false (no .bak sibling)", live)
	}
}

// TestMirrorStockTree_RespectsNeutralizedBak recreates the exact 2026-09-01 regression: a
// deliberately-neutralized Android.mk (renamed to Android.mk.bak after hand-converting its module
// to a sibling Android.bp) must NOT be resurrected by a stock-revival mirror pass, or the
// resurrected Android.mk's LOCAL_MODULE collides with the Android.bp's module of the same name.
func TestMirrorStockTree_RespectsNeutralizedBak(t *testing.T) {
	src := t.TempDir()
	out := t.TempDir()

	srcSub := filepath.Join(src, "hardware", "google", "graphics", "common", "hwc3")
	if err := os.MkdirAll(srcSub, 0o755); err != nil {
		t.Fatal(err)
	}
	makeContent := "LOCAL_MODULE := android.hardware.composer.hwc3-service.pixel\n"
	if err := os.WriteFile(filepath.Join(srcSub, "Android.mk"), []byte(makeContent), 0o644); err != nil {
		t.Fatal(err)
	}

	outSub := filepath.Join(out, "hardware", "google", "graphics", "common", "hwc3")
	if err := os.MkdirAll(outSub, 0o755); err != nil {
		t.Fatal(err)
	}
	// The neutralization: the real Android.mk got renamed to Android.mk.bak, and a hand-converted
	// Android.bp declaring the SAME module now lives at the active path.
	if err := os.WriteFile(filepath.Join(outSub, "Android.mk.bak"), []byte(makeContent), 0o644); err != nil {
		t.Fatal(err)
	}
	bpContent := `cc_binary { name: "android.hardware.composer.hwc3-service.pixel" }`
	if err := os.WriteFile(filepath.Join(outSub, "Android.bp"), []byte(bpContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := mirrorStockTree(src, out, "hardware/google/graphics"); err != nil {
		t.Fatalf("mirrorStockTree: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(outSub, "Android.mk")); statErr == nil {
		t.Errorf("mirrorStockTree resurrected Android.mk over its .bak-neutralized sibling — " +
			"this reintroduces a duplicate LOCAL_MODULE/module-name collision with Android.bp")
	}
}
