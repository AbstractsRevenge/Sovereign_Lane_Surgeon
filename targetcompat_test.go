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
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const fakeGlobalGo = `package config

var (
	commonGlobalCflags = []string{"-Wall"}

	// Flags that must not appear in any command line.
	IllegalFlags = []string{
		"-w",
		"-pedantic",
		"-Werror=pedantic",
	}

	CStdVersion = "gnu23"
)
`

func TestSoongIllegalFlagsFromGoAST(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{soongGlobalCflagsRel: fakeGlobalGo})
	got, err := soongIllegalFlags(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"-w", "-pedantic", "-Werror=pedantic"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if _, err := soongIllegalFlags(t.TempDir()); err == nil {
		t.Fatal("missing global.go must be an error, not an empty list")
	}
}

func TestMoveSystemPropsToProduct(t *testing.T) {
	in := "PRODUCT_SYSTEM_PROPERTIES += ro.launcher.blur.appLaunch=0\n" +
		"  PRODUCT_SYSTEM_PROPERTIES += \\\n    persist.x=false\n" +
		"PRODUCT_SYSTEM_PROPERTIES_EXTRA := 1\n" +
		"FOO := $(PRODUCT_SYSTEM_PROPERTIES)\n" +
		"PRODUCT_VENDOR_PROPERTIES += a=b\n"
	out, ch := moveSystemPropsToProduct([]byte(in))
	if !ch {
		t.Fatal("expected a change")
	}
	want := "PRODUCT_PRODUCT_PROPERTIES += ro.launcher.blur.appLaunch=0\n" +
		"  PRODUCT_PRODUCT_PROPERTIES += \\\n    persist.x=false\n" +
		"PRODUCT_SYSTEM_PROPERTIES_EXTRA := 1\n" +
		"FOO := $(PRODUCT_SYSTEM_PROPERTIES)\n" +
		"PRODUCT_VENDOR_PROPERTIES += a=b\n"
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
	if _, ch2 := moveSystemPropsToProduct(out); ch2 {
		t.Fatal("not idempotent")
	}
}

func TestParseAidlPin(t *testing.T) {
	cases := map[string]string{
		"android.hardware.health-V4-ndk":             "android.hardware.health V4 ndk",
		"com.google.hardware.pixel.display-V13-java": "com.google.hardware.pixel.display V13 java",
		"android.hardware.health-translate-ndk":      "",
		"libbase":                                    "",
		"foo-V-ndk":                                  "",
		"-V1-ndk":                                    "",
	}
	for in, want := range cases {
		p, ok := parseAidlPin(in)
		got := ""
		if ok {
			got = fmt.Sprintf("%s V%d %s", p.Iface, p.Ver, p.Lang)
			if p.String() != in {
				t.Errorf("%q: String() round-trip gave %q", in, p.String())
			}
		}
		if got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestAidlRepinMapSiblingConflictOnly(t *testing.T) {
	idx := aidlSiblings{
		"android.hardware.health": {
			"android.hardware.health":               {},
			"android.hardware.health-translate-ndk": {"android.hardware.health-V5-ndk": true},
		},
		"android.hardware.graphics.composer3": {
			"android.hardware.graphics.composer3-command-buffer": {"android.hardware.graphics.composer3-V4-ndk": true},
		},
	}
	// libpixelhealth: pins V4 and links the translate lib that carries V5 → re-pin.
	got := aidlRepinMap(map[string]bool{"android.hardware.health-V4-ndk": true, "android.hardware.health-translate-ndk": true, "libbase": true}, idx)
	if want := map[string]string{"android.hardware.health-V4-ndk": "android.hardware.health-V5-ndk"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("health: got %v want %v", got, want)
	}
	// hwc3: pins V4 and links a sibling that ALSO pins V4 → no conflict, untouched.
	if got := aidlRepinMap(map[string]bool{"android.hardware.graphics.composer3-V4-ndk": true, "android.hardware.graphics.composer3-command-buffer": true}, idx); len(got) != 0 {
		t.Fatalf("composer3: unexpected re-pin %v", got)
	}
	// A pin with no sibling linked is a plain older pin — legal, untouched.
	if got := aidlRepinMap(map[string]bool{"android.hardware.health-V4-ndk": true}, idx); len(got) != 0 {
		t.Fatalf("lone pin: unexpected re-pin %v", got)
	}
}

func TestApplyTargetCompatOnTree(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		soongGlobalCflagsRel: fakeGlobalGo,
		fsgenArtifactReqRel:  "package fsgen\n// Check for PRODUCT_SYSTEM_PROPERTIES\n",
		"hardware/interfaces/health/aidl/Android.bp": `aidl_interface {
    name: "android.hardware.health",
    frozen: true,
}

cc_library {
    name: "android.hardware.health-translate-ndk",
    shared_libs: ["android.hardware.health-V5-ndk"],
}
`,
		"hardware/google/pixel/health/Android.bp": `cc_library {
    name: "libpixelhealth",
    whole_static_libs: ["android.hardware.health-translate-ndk"],
    shared_libs: [
        "libbase",
        "android.hardware.health-V4-ndk",
    ],
    cflags: ["-Wall", "-pedantic"],
}
`,
		"device/google/gs201/device.mk":         "PRODUCT_SYSTEM_PROPERTIES += ro.launcher.blur.appLaunch=0\n",
		"device/google/gs201/factory_common.mk": "PRODUCT_SYSTEM_PROPERTIES += factory.only=1\n",
		"device/google/other/Android.bp": `cc_binary {
    name: "untouched",
    shared_libs: ["android.hardware.health-V4-ndk"],
}
`,
	})
	r := applyTargetCompat(root, []string{"hardware/google/pixel", "device/google/gs201", "device/google/other"})
	if !reflect.DeepEqual(r.FlagFiles, []string{"hardware/google/pixel/health/Android.bp"}) {
		t.Errorf("flag files: %v", r.FlagFiles)
	}
	if !reflect.DeepEqual(r.PropFiles, []string{"device/google/gs201/device.mk"}) {
		t.Errorf("prop files: %v (factory_common.mk must be left alone)", r.PropFiles)
	}
	if !reflect.DeepEqual(r.AidlRepins, []string{"hardware/google/pixel/health/Android.bp: android.hardware.health-V4-ndk → android.hardware.health-V5-ndk"}) {
		t.Errorf("aidl: %v", r.AidlRepins)
	}
	bp, _ := os.ReadFile(filepath.Join(root, "hardware/google/pixel/health/Android.bp"))
	if s := string(bp); strings.Contains(s, "-pedantic") || strings.Contains(s, "health-V4-ndk") || !strings.Contains(s, "health-V5-ndk") {
		t.Fatalf("health bp not rewritten:\n%s", s)
	}
	other, _ := os.ReadFile(filepath.Join(root, "device/google/other/Android.bp"))
	if !strings.Contains(string(other), "health-V4-ndk") {
		t.Fatal("a lone older pin must not be re-pinned")
	}
	// idempotent
	r2 := applyTargetCompat(root, []string{"hardware/google/pixel", "device/google/gs201", "device/google/other"})
	if len(r2.FlagFiles)+len(r2.PropFiles)+len(r2.AidlRepins) != 0 {
		t.Fatalf("second run changed things: %+v", r2)
	}
}

func TestApplyTargetCompatGatesOnTargetTree(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"device/google/gs201/device.mk":  "PRODUCT_SYSTEM_PROPERTIES += a=1\n",
		"device/google/gs201/Android.bp": "cc_binary {\n    name: \"x\",\n    cflags: [\"-pedantic\"],\n}\n",
	})
	r := applyTargetCompat(root, []string{"device/google/gs201"})
	if len(r.FlagFiles)+len(r.PropFiles)+len(r.AidlRepins) != 0 {
		t.Fatalf("a target without the checks must be left as mirrored: %+v", r)
	}
	if len(r.Notes) < 2 {
		t.Fatalf("expected skip notes, got %v", r.Notes)
	}
}

func TestReferencedGoogleSubtrees(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"fam/aosp_x.mk":        "$(call inherit-product, device/google/gs201/aosp_common.mk)\nPRODUCT_SOONG_NAMESPACES += hardware/google/gchips\n# see device/google/gs201.\n",
		"fam/x/BoardConfig.mk": "include device/google/$(SOC)/BoardConfig-common.mk\nBOARD_SEPOLICY_DIRS += device/google/gs201-sepolicy/x\n",
		"fam/Android.bp":       "cc_defaults {\n    name: \"d\",\n    visibility: [\"//device/google/felix:__subpackages__\"],\n}\n",
		"fam/README.md":        "device/google/ignored-not-a-build-file\n",
	})
	got := referencedGoogleSubtrees(filepath.Join(root, "fam"))
	want := []string{"device/google/felix", "device/google/gs201", "device/google/gs201-sepolicy", "hardware/google/gchips"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
