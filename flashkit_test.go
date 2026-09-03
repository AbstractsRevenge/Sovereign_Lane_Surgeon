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
	"reflect"
	"strings"
	"testing"
)

const cheetahMiscInfo = `ab_update=true
build_super_partition=true
dynamic_partition_list=product system system_dlkm system_ext
lpmake=lpmake
super_block_devices=super
super_google_dynamic_partitions_group_size=8527020032
super_google_dynamic_partitions_partition_list=system system_dlkm system_ext product
super_metadata_device=super
super_partition_groups=google_dynamic_partitions
super_partition_size=8531214336
super_super_device_size=8531214336
use_dynamic_partitions=true
virtual_ab=true
virtual_ab_compression=true
avb_vendor_boot_add_hash_footer_args=--prop x
`

func TestPlanSuperAssemblyAddsPrebuiltVendor(t *testing.T) {
	root := t.TempDir()
	po := filepath.Join(root, "out", "target", "product", "cheetah")
	pre := filepath.Join(root, "vendor", "google_devices", "cheetah", "proprietary")
	writeTree(t, root, map[string]string{
		"out/target/product/cheetah/system.img":                     "s",
		"out/target/product/cheetah/system_ext.img":                 "s",
		"out/target/product/cheetah/product.img":                    "s",
		"out/target/product/cheetah/system_dlkm.img":                "s",
		"vendor/google_devices/cheetah/proprietary/vendor.img":      "v",
		"vendor/google_devices/cheetah/proprietary/vendor_dlkm.img": "v",
	})
	misc, _ := parseMiscInfo([]byte(cheetahMiscInfo))
	plan, err := planSuperAssembly(misc, po, pre)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Added, []string{"vendor", "vendor_dlkm"}) {
		t.Fatalf("added %v", plan.Added)
	}
	if want := "system system_dlkm system_ext product vendor vendor_dlkm"; plan.MiscInfo["super_google_dynamic_partitions_partition_list"] != want || plan.MiscInfo["dynamic_partition_list"] != want {
		t.Fatalf("lists: %v", plan.MiscInfo)
	}
	if plan.MiscInfo["vendor_image"] != filepath.Join(pre, "vendor.img") || plan.MiscInfo["system_image"] != filepath.Join(po, "system.img") {
		t.Fatalf("images: %v", plan.MiscInfo)
	}
	if _, leaked := plan.MiscInfo["avb_vendor_boot_add_hash_footer_args"]; leaked {
		t.Fatal("unrelated misc_info keys must not be copied")
	}
	if plan.MiscInfo["virtual_ab"] != "true" || plan.MiscInfo["super_partition_size"] != "8531214336" {
		t.Fatal("super/virtual_ab keys must be kept")
	}
	// nothing prebuilt → nothing added
	plan2, err := planSuperAssembly(misc, po, filepath.Join(root, "nowhere"))
	if err != nil || len(plan2.Added) != 0 {
		t.Fatalf("plan2 %v %v", plan2.Added, err)
	}
}

func TestFlashScriptEncodesTheLessons(t *testing.T) {
	s := flashScript("cheetah", "/r/out-aosp17/cheetah/eng/target/product/cheetah", "/r/out-aosp17/cheetah/eng/host/linux-x86/bin", "/r/vendor/google_devices/cheetah/proprietary", "/r/super_full.img",
		[]string{"require version-bootloader=cloudripper-17.0-15199429", "require version-baseband=g5300q-260317-260505-B-15346003"})
	for _, want := range []string{"cloudripper-17.0-15199429", "flash vbmeta \"$P/vbmeta.img\"", "format:f2fs userdata", "format:f2fs metadata", "flash super \"/r/super_full.img\"", "H=/r/out-aosp17/cheetah/eng/host/linux-x86/bin"} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q", want)
		}
	}
	if strings.Contains(strings.ReplaceAll(s, "NEVER --disable-verification", ""), "--disable-verification") {
		t.Error("script must not use --disable-verification")
	}
}

func TestEnableAndroidInfoIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "BoardConfigVendor.mk")
	os.WriteFile(p, []byte("-include vendor/google_devices/cheetah/BoardConfigPartial.mk\n"), 0o644)
	if err := enableAndroidInfo(p); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(b), "#") || strings.Count(string(b), "USE_ANDROID_INFO := true") != 1 || !strings.Contains(string(b), "-include vendor/google_devices/cheetah/BoardConfigPartial.mk") {
		t.Fatalf("bad edit:\n%s", b)
	}
	enableAndroidInfo(p)
	b2, _ := os.ReadFile(p)
	if string(b2) != string(b) {
		t.Fatal("not idempotent")
	}
}
