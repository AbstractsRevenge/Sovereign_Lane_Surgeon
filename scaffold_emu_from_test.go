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

// A lane-sourced fork must INHERIT the source lane's emulator product, not restart from the TODO
// template: the app suite, soong namespaces and privapp mode are precisely the fields the template
// cannot know, and getting them wrong shows up as a boot crash-loop or a kati failure, not as a
// missing line in a .mk.
func TestGenEmuProductFromLane(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "device", "generic", "goldfish", "64bitonly", "product")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `PRODUCT_NAME := sdk_phone64_holo
PRODUCT_PRODUCT_PROPERTIES += ro.control_privapp_permissions=log
$(call inherit-product, packages-holo/bootanimation/phone/bootanimation.mk)
SOONG_CONFIG_NAMESPACES += holo_framework_routing holo_package_routing
SOONG_CONFIG_holo_framework_routing += enable_holo_res
PRODUCT_SOONG_NAMESPACES += bootable/deprecated-ota frameworks-holo packages-holo
PRODUCT_PACKAGES += HoloDialer HoloContacts Launcher3
`
	if err := os.WriteFile(filepath.Join(dir, "sdk_phone64_holo.mk"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c := deriveLane("holotest", true, nil, true, false, "")
	c.FromLane = "holo"
	rel, out, ok := genEmuProductFromLane(c, emuForms[0], root, "holo")
	if !ok {
		t.Fatal("expected derivation from the source lane product")
	}
	if !strings.HasSuffix(rel, "sdk_phone64_holotest.mk") {
		t.Errorf("bad path: %s", rel)
	}
	for _, want := range []string{
		"PRODUCT_NAME := sdk_phone64_holotest",
		"ro.control_privapp_permissions=log",    // inherited, not a TODO
		"packages-holotest/bootanimation",       // repointed
		"holotest_framework_routing",            // namespace repointed
		"enable_holotest_res",                   // variable repointed
		"bootable/deprecated-ota",               // inherited
		"frameworks-holotest packages-holotest", // repointed
		"HoloDialer HoloContacts Launcher3",     // app suite inherited verbatim
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "_holo\n") || strings.Contains(out, "-holo/") || strings.Contains(out, " holo_") {
		t.Errorf("source-lane token survived:\n%s", out)
	}
	// absent source product => caller falls back to the template
	if _, _, ok := genEmuProductFromLane(c, emuForms[0], root, "nosuchlane"); ok {
		t.Error("must report not-ok when the source product is absent")
	}
}
