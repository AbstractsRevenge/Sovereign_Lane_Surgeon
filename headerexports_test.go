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

const dmabufheapNoExport = "cc_library {\n    name: \"libdmabufheap\",\n    export_include_dirs: [\"include\"],\n    shared_libs: [\"libbase\"],\n}\n"
const dmabufheapExports = "cc_library {\n    name: \"libdmabufheap\",\n    static_libs: [\"libion\"],\n    export_static_lib_headers: [\"libion\"],\n}\n"
const libionBp = "cc_library_headers {\n    name: \"libion_headers\",\n    vendor_available: true,\n}\n"
const ionConsumerBp = "cc_library {\n    name: \"libion_google\",\n    proprietary: true,\n    srcs: [\n        \"ion.cpp\",\n    ],\n    shared_libs: [\"liblog\", \"libdmabufheap\"],\n}\n\ncc_library {\n    name: \"other\",\n    srcs: [\"other.cpp\"],\n    shared_libs: [\"libdmabufheap\"],\n}\n"

func TestAddLostHeaderLibs(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"system/memory/libdmabufheap/Android.bp":            dmabufheapNoExport,
		"system/memory/libion/Android.bp":                   libionBp,
		"hardware/google/graphics/common/libion/Android.bp": ionConsumerBp,
		"hardware/google/graphics/common/libion/ion.cpp":    "#include <ion/ion.h>\n",
		"hardware/google/graphics/common/libion/other.cpp":  "#include <log/log.h>\n",
	})
	var r compatReport
	addLostHeaderLibs(root, []string{"hardware/google/graphics"}, &r)
	if len(r.HeaderLibs) != 1 || !strings.HasPrefix(r.HeaderLibs[0], "hardware/google/graphics/common/libion/Android.bp: libion_google += header_libs libion_headers") {
		t.Fatalf("report %v", r.HeaderLibs)
	}
	b, _ := os.ReadFile(filepath.Join(root, "hardware/google/graphics/common/libion/Android.bp"))
	s := string(b)
	if strings.Count(s, "libion_headers") != 1 || !strings.Contains(s, "name: \"other\"") {
		t.Fatalf("bad edit:\n%s", s)
	}
	var r2 compatReport
	addLostHeaderLibs(root, []string{"hardware/google/graphics"}, &r2)
	if len(r2.HeaderLibs) != 0 {
		t.Fatal("not idempotent")
	}
}

func TestAddLostHeaderLibsInertWhenProviderExports(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"system/memory/libdmabufheap/Android.bp":            dmabufheapExports,
		"system/memory/libion/Android.bp":                   libionBp,
		"hardware/google/graphics/common/libion/Android.bp": ionConsumerBp,
		"hardware/google/graphics/common/libion/ion.cpp":    "#include <ion/ion.h>\n",
	})
	var r compatReport
	addLostHeaderLibs(root, []string{"hardware/google/graphics"}, &r)
	if len(r.HeaderLibs) != 0 {
		t.Fatalf("an android-15 style provider must leave the module alone: %v", r.HeaderLibs)
	}
}
