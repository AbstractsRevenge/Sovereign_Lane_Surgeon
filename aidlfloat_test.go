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

const powerAidlBp17 = `power_version = "android.hardware.power-V7"

aidl_interface {
    name: "android.hardware.power",
    versions_with_info: [
        {version: "1"}, {version: "2"}, {version: "3"}, {version: "4"}, {version: "5"}, {version: "6"}, {version: "7"},
    ],
    frozen: true,
}

cc_defaults {
    name: "android.hardware.power-ndk_shared",
    shared_libs: [
        power_version + "-ndk",
    ],
}

cc_defaults {
    name: "android.hardware.power-ndk_static",
    static_libs: [
        power_version + "-ndk",
    ],
}
`

const halBp = `cc_binary {
    name: "android.hardware.power-service.pixel-libperfmgr",
    defaults: ["android.hardware.power-ndk_shared", "other_defaults"],
    vintf_fragments: ["aidl/android.hardware.power-service.pixel.xml"],
    shared_libs: ["libbase"],
}

cc_test {
    name: "libadpf_test",
    defaults: ["android.hardware.power-ndk_static"],
}

cc_binary {
    name: "lights",
    defaults: ["android.hardware.graphics.common-ndk_static"],
}
`

const powerFragment = `<manifest version="1.0" type="device">
    <hal format="aidl">
        <name>android.hardware.power</name>
        <version>6</version>
        <fqname>IPower/default</fqname>
    </hal>
</manifest>
`

func TestPinFloatingAidlDefaults(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"hardware/interfaces/power/aidl/Android.bp":                                            powerAidlBp17,
		"hardware/google/pixel/power-libperfmgr/Android.bp":                                    halBp,
		"hardware/google/pixel/power-libperfmgr/aidl/android.hardware.power-service.pixel.xml": powerFragment,
	})
	var r compatReport
	pinFloatingAidlDefaults(root, []string{"hardware/google/pixel"}, &r)
	if len(r.AidlPins) != 2 {
		t.Fatalf("pins %v", r.AidlPins)
	}
	b, _ := os.ReadFile(filepath.Join(root, "hardware/google/pixel/power-libperfmgr/Android.bp"))
	s := string(b)
	if strings.Contains(s, "android.hardware.power-ndk_") || !strings.Contains(s, `"android.hardware.power-V6-ndk"`) {
		t.Fatalf("not pinned:\n%s", s)
	}
	if !strings.Contains(s, `defaults: ["other_defaults"]`) || !strings.Contains(s, `"libbase"`) {
		t.Fatalf("other defaults / libs must survive:\n%s", s)
	}
	if !strings.Contains(s, "android.hardware.graphics.common-ndk_static") {
		t.Fatal("an interface the fragment does not declare must keep floating")
	}
	if !strings.Contains(s, "static_libs: [\"android.hardware.power-V6-ndk\"]") && !strings.Contains(s, "static_libs: [\n        \"android.hardware.power-V6-ndk\",\n    ]") {
		t.Fatalf("_static must pin via static_libs:\n%s", s)
	}
	var r2 compatReport
	pinFloatingAidlDefaults(root, []string{"hardware/google/pixel"}, &r2)
	if len(r2.AidlPins) != 0 {
		t.Fatal("not idempotent")
	}
	// A target whose defaults already resolve to the declared version: untouched.
	root2 := t.TempDir()
	writeTree(t, root2, map[string]string{
		"hardware/interfaces/power/aidl/Android.bp":                                            strings.Replace(powerAidlBp17, `power_version = "android.hardware.power-V7"`, `power_version = "android.hardware.power-V6"`, 1),
		"hardware/google/pixel/power-libperfmgr/Android.bp":                                    halBp,
		"hardware/google/pixel/power-libperfmgr/aidl/android.hardware.power-service.pixel.xml": powerFragment,
	})
	var r3 compatReport
	pinFloatingAidlDefaults(root2, []string{"hardware/google/pixel"}, &r3)
	if len(r3.AidlPins) != 0 {
		t.Fatalf("same version → no pin: %v", r3.AidlPins)
	}
}
