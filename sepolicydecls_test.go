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

func TestSepolicyDeclaredType(t *testing.T) {
	cases := map[string]string{
		"vendor_internal_prop(vendor_chre_hal_prop)":             "vendor_chre_hal_prop",
		"type vendor_foo, property_type, vendor_property_type;":  "vendor_foo",
		"type hal_gnss_pixel;":                                   "hal_gnss_pixel",
		"set_prop(vendor_init, vendor_chre_hal_prop)":            "",
		"typeattribute foo bar;":                                 "",
		"allow foo bar:file read;":                               "",
		"# vendor_internal_prop(commented)":                      "",
		"get_prop(hal_contexthub_default, vendor_chre_hal_prop)": "",
		"system_vendor_config_prop(vendor_x_prop)":               "vendor_x_prop",
	}
	for in, want := range cases {
		if got := sepolicyDeclaredType(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestDropPlatformDeclaredTypes(t *testing.T) {
	root := t.TempDir()
	te := "# CHRE\nvendor_internal_prop(vendor_chre_hal_prop)\nvendor_internal_prop(vendor_only_prop)\nset_prop(vendor_init, vendor_chre_hal_prop)\n"
	writeTree(t, root, map[string]string{
		"system/sepolicy/vendor/property.te":                   "vendor_internal_prop(vendor_chre_hal_prop)\n",
		"device/google/gs-common/chre/sepolicy/property.te":    te,
		"device/google/gs-common/chre/sepolicy/vendor_init.te": "set_prop(vendor_init, vendor_chre_hal_prop)\n",
	})
	src := t.TempDir() // the pristine source of the mirror
	writeTree(t, src, map[string]string{"device/google/gs-common/chre/sepolicy/property.te": te})
	var r compatReport
	dropPlatformDeclaredTypesFrom(root, []string{"device/google/gs-common"}, os.DirFS(src), &r)
	if len(r.SepolicyDrops) != 1 || !strings.Contains(r.SepolicyDrops[0], "property.te: dropped declaration of vendor_chre_hal_prop") {
		t.Fatalf("report %v", r.SepolicyDrops)
	}
	b, _ := os.ReadFile(filepath.Join(root, "device/google/gs-common/chre/sepolicy/property.te"))
	if s := string(b); strings.Contains(s, "vendor_internal_prop(vendor_chre_hal_prop)") || !strings.Contains(s, "vendor_internal_prop(vendor_only_prop)") || !strings.Contains(s, "set_prop(vendor_init, vendor_chre_hal_prop)") || !strings.Contains(s, "# CHRE") {
		t.Fatalf("bad edit:\n%s", s)
	}
	var r2 compatReport
	dropPlatformDeclaredTypesFrom(root, []string{"device/google/gs-common"}, os.DirFS(src), &r2)
	if len(r2.SepolicyDrops) != 0 {
		t.Fatal("not idempotent")
	}
	// no system/sepolicy → untouched
	root2 := t.TempDir()
	writeTree(t, root2, map[string]string{"device/google/gs-common/chre/sepolicy/property.te": te})
	var r3 compatReport
	dropPlatformDeclaredTypesFrom(root2, []string{"device/google/gs-common"}, os.DirFS(src), &r3)
	if len(r3.SepolicyDrops) != 0 || len(r3.Notes) != 1 {
		t.Fatalf("without a platform policy nothing may change: %+v", r3)
	}
}

func TestDropPlatformPrivateTypeRulesAndContexts(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"system/sepolicy/private/file.te":              "type per_boot_file, file_type, data_file_type, core_data_file_type;\n",
		"system/sepolicy/private/file_contexts":        "/data/per_boot(/.*)?      u:object_r:per_boot_file:s0\n",
		"system/sepolicy/public/file.te":               "type vendor_visible, file_type;\n",
		"device/google/gs201-sepolicy/x/file.te":       "type per_boot_file, file_type, data_file_type, core_data_file_type;\ntype vendor_visible, file_type;\ntype my_own_file, file_type;\n",
		"device/google/gs201-sepolicy/x/kernel.te":     "# ZRam\nallow kernel per_boot_file:file r_file_perms;\nallow kernel my_own_file:file {\n    read\n    open\n};\nallow kernel vendor_visible:file read;\n",
		"device/google/gs201-sepolicy/x/init.te":       "allowxperm init per_boot_file:file ioctl { F2FS_IOC_SET_PIN_FILE };\nallow init per_boot_file:file {\n  ioctl\n};\nallow init foo:file read;\n",
		"device/google/gs201-sepolicy/x/file_contexts": "/data/per_boot(/.*)?   u:object_r:per_boot_file:s0\n/data/other(/.*)?      u:object_r:per_boot_file:s0\n/vendor/x              u:object_r:my_own_file:s0\n",
	})
	src := t.TempDir()
	writeTree(t, src, map[string]string{"device/google/gs201-sepolicy/x/file.te": "type per_boot_file, file_type, data_file_type, core_data_file_type;\ntype vendor_visible, file_type;\ntype my_own_file, file_type;\n"})
	var r compatReport
	dropPlatformDeclaredTypesFrom(root, []string{"device/google/gs201-sepolicy"}, os.DirFS(src), &r)
	read := func(rel string) string { b, _ := os.ReadFile(filepath.Join(root, rel)); return string(b) }
	if s := read("device/google/gs201-sepolicy/x/file.te"); strings.Contains(s, "per_boot_file") || strings.Contains(s, "type vendor_visible") || !strings.Contains(s, "type my_own_file") {
		t.Fatalf("file.te:\n%s", s)
	}
	if s := read("device/google/gs201-sepolicy/x/kernel.te"); strings.Contains(s, "per_boot_file") || !strings.Contains(s, "# ZRam") || !strings.Contains(s, "my_own_file:file {\n    read\n    open\n};") || !strings.Contains(s, "allow kernel vendor_visible:file read;") {
		t.Fatalf("kernel.te (vendor-visible type rules must stay, private ones go):\n%s", s)
	}
	if s := read("device/google/gs201-sepolicy/x/init.te"); strings.Contains(s, "per_boot_file") || !strings.Contains(s, "allow init foo:file read;") {
		t.Fatalf("init.te (multi-line rule must go whole):\n%s", s)
	}
	if s := read("device/google/gs201-sepolicy/x/file_contexts"); strings.Contains(s, "/data/per_boot(") || !strings.Contains(s, "/data/other(/.*)?") || !strings.Contains(s, "/vendor/x") {
		t.Fatalf("file_contexts (only the platform-carried path spec goes):\n%s", s)
	}
	var r2 compatReport
	dropPlatformDeclaredTypesFrom(root, []string{"device/google/gs201-sepolicy"}, os.DirFS(src), &r2)
	if len(r2.SepolicyDrops) != 0 {
		t.Fatalf("not idempotent: %v", r2.SepolicyDrops)
	}
}

// A tree treated by an earlier version that dropped only the declaration must still lose the
// rules on the next run — the candidates come from the pristine source, not the target.
func TestDropPlatformPrivateTypeRulesOnRerun(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"system/sepolicy/private/file.te":          "type per_boot_file, file_type;\n",
		"system/sepolicy/private/file_contexts":    "/data/per_boot(/.*)?      u:object_r:per_boot_file:s0\n",
		"device/google/gs201-sepolicy/x/file.te":   "type my_own_file, file_type;\n", // declaration already gone
		"device/google/gs201-sepolicy/x/kernel.te": "allow kernel per_boot_file:file r_file_perms;\n",
	})
	src := t.TempDir()
	writeTree(t, src, map[string]string{"device/google/gs201-sepolicy/x/file.te": "type per_boot_file, file_type;\ntype my_own_file, file_type;\n"})
	var r compatReport
	dropPlatformDeclaredTypesFrom(root, []string{"device/google/gs201-sepolicy"}, os.DirFS(src), &r)
	b, _ := os.ReadFile(filepath.Join(root, "device/google/gs201-sepolicy/x/kernel.te"))
	if strings.Contains(string(b), "per_boot_file") {
		t.Fatalf("rule survived a re-run: %q", b)
	}
}
