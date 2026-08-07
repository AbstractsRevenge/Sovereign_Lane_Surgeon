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
	"strings"
	"testing"
)

// TestDropDepBp: dropDepsBp removes exactly the named external-label + bare-name entries from static_libs
// (and leaves everything else — including the module name and unrelated deps — intact). Mirrors the no-Compose
// SystemUI fix: drop the stale //<lane>/libs/systemui:X refs the app resolves internally.
func TestDropDepBp(t *testing.T) {
	const src = `android_library {
    name: "NexusMPlatformAnimationLib",
    srcs: [
        "src/**/*.kt",
    ],
    static_libs: [
        "androidx.core_core-animation",
        "NexusMSystemUIShaderLib",
        "//frameworks-nexusm/libs/systemui:animationlib",
        "//frameworks-nexusm/libs/systemui:com_android_systemui_shared_flags_lib",
    ],
}
`
	drop := map[string]bool{
		"//frameworks-nexusm/libs/systemui:animationlib":                        true,
		"//frameworks-nexusm/libs/systemui:com_android_systemui_shared_flags_lib": true,
	}
	out, changed, err := dropDepsBp([]byte(src), drop)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	got := string(out)
	if strings.Contains(got, "//frameworks-nexusm/libs/systemui:") {
		t.Errorf("external systemui refs survived:\n%s", got)
	}
	// The module name + unrelated deps must be preserved.
	for _, keep := range []string{`name: "NexusMPlatformAnimationLib"`, "androidx.core_core-animation", "NexusMSystemUIShaderLib"} {
		if !strings.Contains(got, keep) {
			t.Errorf("dropped something it shouldn't have (%q missing):\n%s", keep, got)
		}
	}
}

// TestDropDepIdempotent: nothing to drop → no change, byte-identical.
func TestDropDepIdempotent(t *testing.T) {
	const src = `java_library {
    name: "x",
    static_libs: ["a", "b"],
}
`
	out, changed, err := dropDepsBp([]byte(src), map[string]bool{"//nope:z": true})
	if err != nil {
		t.Fatal(err)
	}
	if changed || string(out) != src {
		t.Error("expected no-op on an unaffected bp")
	}
}
