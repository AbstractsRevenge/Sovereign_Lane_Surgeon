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
	"path/filepath"
	"strings"
	"testing"
)

// A target tree with the three facts the detectors look up, and a log with the three failures.
func TestCompatProposeDerivesRowsFromFailures(t *testing.T) {
	root := t.TempDir()
	// header: libion_headers owns ion/ion.h; the failing module depends on libdmabufheap
	write(t, filepath.Join(root, "system/memory/libion/Android.bp"), "cc_library_headers {\n    name: \"libion_headers\",\n    vendor_available: true,\n    export_include_dirs: [\"include\"],\n}\n")
	write(t, filepath.Join(root, "system/memory/libion/include/ion/ion.h"), "#pragma once\n")
	write(t, filepath.Join(root, "system/memory/libdmabufheap/Android.bp"), "cc_library {\n    name: \"libdmabufheap\",\n}\n")
	write(t, filepath.Join(root, "hardware/google/graphics/common/libion/Android.bp"), "cc_library_shared {\n    name: \"libion_vendor\",\n    srcs: [\"ion.cpp\"],\n    shared_libs: [\"liblog\", \"libdmabufheap\"],\n}\n")
	// proto: the defining proto renamed module → module_name
	write(t, filepath.Join(root, "frameworks/proto_logging/stats/atom_field_options.proto"), "package android.os.statsd;\nextend google.protobuf.FieldOptions {\n  // reserved 50004; module has been moved to module_name\n  repeated string module_name = 50010;\n  optional bool restriction_category = 50011;\n}\n")
	// sepolicy: the platform rule and the mirrored statement
	plat := "neverallow dumpstate {\n  vold\n}:binder call;\n"
	write(t, filepath.Join(root, "system/sepolicy/private/dumpstate.te"), strings.Repeat("# pad\n", 487)+plat)
	write(t, filepath.Join(root, "device/google/gs-common/storage/sepolicy/dumpstate.te"), "allow dumpstate vold:binder { call };\n")
	write(t, filepath.Join(root, "device/google/gs-common/camera/sepolicy/vendor/vendor_pcs_app.te"), "binder_call(vendor_pcs_app, hal_pixel_remote_camera_service);\n")

	log := []string{
		"FAILED: out/soong/.intermediates/hardware/google/graphics/common/libion/libion_vendor/android_vendor.35_arm64_armv8-a_shared/obj/ion.o",
		"hardware/google/graphics/common/libion/ion.cpp:20:10: fatal error: 'ion/ion.h' file not found",
		"pixelatoms.proto:40:5: Option \"(android.os.statsd.module)\" unknown. Ensure that your proto definition file imports the proto which defines the option.",
		"libsepol.report_failure: neverallow on line 488 of system/sepolicy/private/dumpstate.te (or line 45678 of policy.conf) violated by allow dumpstate vold:binder { call };",
		"libsepol.report_failure: neverallow on line 488 of system/sepolicy/private/dumpstate.te (or line 45678 of policy.conf) violated by allow dumpstate vold:binder { call };",
		"libsepol.report_failure: neverallow on line 10 of system/sepolicy/private/domain.te (or line 999 of policy.conf) violated by allow vendor_pcs_app hal_pixel_remote_camera_service:binder { call transfer };",
	}
	write(t, filepath.Join(root, "system/sepolicy/private/domain.te"), strings.Repeat("\n", 9)+"neverallow * { -domain }:binder *;\n")
	props := proposeCompat(root, log)
	var rows []string
	for _, p := range props {
		if p.Row != "" {
			rows = append(rows, p.Manifest+"|"+p.Row)
		} else {
			t.Logf("no row: %s", p.Why)
		}
	}
	want := []string{
		"header_exports.MANIFEST|libdmabufheap\tsystem/memory/libdmabufheap/Android.bp\tlibion\tion/\tlibion_headers\tsystem/memory/libion/Android.bp",
		"proto_options.MANIFEST|frameworks/proto_logging/stats/atom_field_options.proto\tandroid.os.statsd\tmodule\tmodule_name",
		"sepolicy_neverallow/MANIFEST|device/google/gs-common/storage/sepolicy/dumpstate.te\tallow dumpstate vold:binder { call };\tsystem/sepolicy/private/dumpstate.te\tneverallow dumpstate {",
		"sepolicy_neverallow/MANIFEST|device/google/gs-common/camera/sepolicy/vendor/vendor_pcs_app.te\tbinder_call(vendor_pcs_app, hal_pixel_remote_camera_service);\tsystem/sepolicy/private/domain.te\tneverallow * { -domain }:binder *;",
	}
	for _, w := range want {
		found := false
		for _, r := range rows {
			if strings.Contains(r, w) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing proposal containing %q\nrows:\n%s", w, strings.Join(rows, "\n"))
		}
	}
	if len(rows) != 4 {
		t.Errorf("expected exactly 4 rows, got %d:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	// The proposed rows parse back into the tables the operations consume.
	for _, r := range rows {
		row := r[strings.Index(r, "|")+1:]
		switch {
		case strings.HasPrefix(r, "header_exports"):
			if e := parseHeaderExports(row); len(e) != 1 || e[0].HeaderLib != "libion_headers" {
				t.Errorf("header row does not parse: %q", row)
			}
		case strings.HasPrefix(r, "proto_options"):
			if e := parseProtoOptions(row); len(e) != 1 || e[0].New != "module_name" {
				t.Errorf("proto row does not parse: %q", row)
			}
		}
	}
}

func TestEmbeddedCompatManifestsParse(t *testing.T) {
	if len(lostHeaderExports) < 1 || lostHeaderExports[0].HeaderLib != "libion_headers" {
		t.Errorf("header_exports.MANIFEST: %+v", lostHeaderExports)
	}
	if len(renamedProtoOptions) < 1 || renamedProtoOptions[0].New != "module_name" {
		t.Errorf("proto_options.MANIFEST: %+v", renamedProtoOptions)
	}
}
