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

func TestKernelHeadersBundle(t *testing.T) {
	for _, fam := range []string{"pantah", "lynx", "tangorpro", "felix", "raviole", "bluejay", "shusky", "akita", "caimito", "comet", "tegu"} {
		tag, build := kernelHeadersProvenance(fam)
		if tag == "" || build == "" {
			t.Errorf("%s: no provenance", fam)
		}
		if _, err := embeddedKernelHeaders.ReadFile("assets/kernel_headers/" + fam + "/kernel-headers/drm/samsung_drm.h"); err != nil {
			t.Errorf("%s: samsung_drm.h missing from the bundle", fam)
		}
	}
	out := t.TempDir()
	k := &kernelAssembly{Dir: "device/google/pantah-kernels/6.1/x"}
	if n := putKernelHeaders(k, out, "pantah"); n < 9 {
		t.Fatalf("pantah headers: %d files", n)
	}
	if _, err := os.Stat(filepath.Join(out, k.Dir, "kernel-headers", "drm", "samsung_drm.h")); err != nil {
		t.Fatal("samsung_drm.h not written")
	}
	if !strings.Contains(strings.Join(k.Notes, "\n"), "android-15.0.0_r36") {
		t.Fatalf("provenance note missing: %v", k.Notes)
	}
	k2 := &kernelAssembly{Dir: "device/google/unknown-kernels/6.1/x"}
	if n := putKernelHeaders(k2, out, "unknownfam"); n != 0 || !strings.Contains(strings.Join(k2.Notes, "\n"), "none bundled") {
		t.Fatalf("unknown family must be reported: n=%d notes=%v", n, k2.Notes)
	}
}
