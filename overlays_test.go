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

const fakeDenylistGo = `package build

var androidmk_denylist []string = []string{
	"device/google/",
	"hardware/google/",
}

var androidmk_allowlist []string = []string{
	"hardware/google/allowed/Android.mk",
}
`

func TestOverlayManifestAndEmbeddedFiles(t *testing.T) {
	entries := embeddedOverlayEntries()
	if len(entries) == 0 {
		t.Fatal("no overlay entries")
	}
	for _, e := range entries {
		if e.Subtree == "" || !strings.Contains(e.Source, "@") {
			t.Errorf("bad entry %+v", e)
		}
		if _, err := embeddedOverlays.ReadDir("assets/overlays/" + e.Subtree); err != nil {
			t.Errorf("overlay %s has no embedded files: %v", e.Subtree, err)
		}
	}
	if ov, ok := overlayFor("hardware/google/graphics/common/hwc3/Android.mk", entries); !ok || ov.Subtree != "hardware/google/graphics/common" {
		t.Fatalf("overlayFor: %+v %v", ov, ok)
	}
	if _, ok := overlayFor("hardware/google/graphics/commonx/Android.mk", entries); ok {
		t.Fatal("prefix must match on a path boundary")
	}
}

func TestDenylistFromGoASTAndAllowlist(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{androidmkDenylistRel: fakeDenylistGo})
	deny, allow, err := soongAndroidmkDenylist(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(deny, []string{"device/google/", "hardware/google/"}) || !reflect.DeepEqual(allow, []string{"hardware/google/allowed/Android.mk"}) {
		t.Fatalf("deny=%v allow=%v", deny, allow)
	}
	if !androidmkBlocked("hardware/google/graphics/common/Android.mk", deny, allow) {
		t.Fatal("should be blocked")
	}
	if androidmkBlocked("hardware/google/allowed/Android.mk", deny, allow) || androidmkBlocked("vendor/x/Android.mk", deny, allow) {
		t.Fatal("allowlisted / undenylisted must pass")
	}
}

func TestReplaceDenylistedAndroidMk(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		androidmkDenylistRel:                                   fakeDenylistGo,
		"hardware/google/graphics/common/Android.mk":           "include $(call all-subdir-makefiles)\n",
		"hardware/google/graphics/common/Android.bp":           "// r36 root bp\n",
		"hardware/google/graphics/common/hwc3/Android.mk":      "LOCAL_MODULE := hwc3\n",
		"hardware/google/graphics/common/libhwc2.1/Android.mk": "LOCAL_MODULE := libExynosHWCService\n",
		"hardware/google/graphics/common/libhwc2.1/Android.bp": "// r36 libhwc2.1 bp\n",
		"hardware/google/graphics/common/libion/Android.bp":    "cc_library { name: \"libion_exynos\" }\n",
		"device/google/nooverlay/Android.mk":                   "LOCAL_MODULE := x\n",
		"vendor/notdenied/Android.mk":                          "LOCAL_MODULE := y\n",
	})
	roots := []string{"hardware/google/graphics", "device/google/nooverlay", "vendor/notdenied"}
	var r compatReport
	replaceDenylistedAndroidMk(root, roots, &r)
	if !reflect.DeepEqual(r.Overlays, []string{"hardware/google/graphics/common ← android.googlesource.com/platform/hardware/google/graphics/common@562ede8"}) {
		t.Fatalf("overlays: %v", r.Overlays)
	}
	if !reflect.DeepEqual(r.MkRemoved, []string{
		"hardware/google/graphics/common/Android.mk",
		"hardware/google/graphics/common/hwc3/Android.mk",
		"hardware/google/graphics/common/libhwc2.1/Android.mk",
	}) {
		t.Fatalf("removed: %v", r.MkRemoved)
	}
	if !reflect.DeepEqual(r.BlockedMk, []string{"device/google/nooverlay/Android.mk"}) {
		t.Fatalf("blocked: %v", r.BlockedMk)
	}
	for _, rel := range r.MkRemoved {
		if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
			t.Errorf("%s still present", rel)
		}
	}
	want, _ := embeddedOverlays.ReadFile("assets/overlays/hardware/google/graphics/common/hwc3/Android.bp")
	got, err := os.ReadFile(filepath.Join(root, "hardware/google/graphics/common/hwc3/Android.bp"))
	if err != nil || string(got) != string(want) {
		t.Fatalf("hwc3/Android.bp not written from the overlay (err=%v)", err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "hardware/google/graphics/common/libion/Android.bp")); !strings.Contains(string(b), "libion_exynos") {
		t.Fatal("a file outside the overlay's set must be untouched")
	}
	if _, err := os.Stat(filepath.Join(root, "device/google/nooverlay/Android.mk")); err != nil {
		t.Fatal("a blocked makefile without an overlay must be left in place (reported, not deleted)")
	}
	if _, err := os.Stat(filepath.Join(root, "vendor/notdenied/Android.mk")); err != nil {
		t.Fatal("an undenylisted makefile must be left alone")
	}
	var r2 compatReport
	replaceDenylistedAndroidMk(root, roots, &r2)
	if len(r2.Overlays)+len(r2.MkRemoved)+len(r2.OverlayFiles) != 0 {
		t.Fatalf("not idempotent: %+v", r2)
	}
}

func TestReplaceDenylistedAndroidMkNoDenylist(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"hardware/google/graphics/common/Android.mk": "x\n"})
	var r compatReport
	replaceDenylistedAndroidMk(root, []string{"hardware/google/graphics"}, &r)
	if len(r.Overlays) != 0 || len(r.Notes) != 1 {
		t.Fatalf("a target without a denylist must be left as mirrored: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, "hardware/google/graphics/common/Android.mk")); err != nil {
		t.Fatal("makefile removed without a denylist")
	}
}

func TestMaterializeSkipsOverlayRemovedMakefiles(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{androidmkDenylistRel: fakeDenylistGo})
	delete(overlayRemovesCache, root)
	if _, err := materializeEmbedded("hardware/google/graphics/common", root); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"hardware/google/graphics/common/Android.mk", "hardware/google/graphics/common/hwc3/Android.mk"} {
		if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
			t.Errorf("%s was materialized although the target denylists it and an overlay replaces it", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "hardware/google/graphics/common/libion/Android.bp")); err != nil {
		t.Error("the rest of the subtree must still land")
	}
	// Without a denylist (android-15 target) the makefiles land as mirrored.
	root2 := t.TempDir()
	if _, err := materializeEmbedded("hardware/google/graphics/common", root2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root2, "hardware/google/graphics/common/hwc3/Android.mk")); err != nil {
		t.Error("a target that accepts the makefiles must receive them")
	}
}

func TestVestigialIncludeOnlyMk(t *testing.T) {
	root := t.TempDir()
	wrapper := "LOCAL_PATH := $(call my-dir)\nifneq (,$(filter $(TARGET_DEVICE),tegu))\n  include $(call all-makefiles-under,$(LOCAL_PATH))\nendif\n"
	writeTree(t, root, map[string]string{
		androidmkDenylistRel:                  fakeDenylistGo,
		"device/google/tegu/Android.mk":       wrapper,
		"device/google/tegu/audio/Android.bp": "// bp only\n",
		"device/google/real/Android.mk":       "LOCAL_PATH := $(call my-dir)\ninclude $(CLEAR_VARS)\nLOCAL_MODULE := x\ninclude $(BUILD_PREBUILT)\n",
		"device/google/wrap2/Android.mk":      wrapper,
		"device/google/wrap2/sub/Android.mk":  "LOCAL_MODULE := y\n",
	})
	var r compatReport
	replaceDenylistedAndroidMk(root, []string{"device/google/tegu", "device/google/real", "device/google/wrap2"}, &r)
	if _, err := os.Stat(filepath.Join(root, "device/google/tegu/Android.mk")); err == nil {
		t.Error("the empty wrapper must be removed")
	}
	if len(r.MkRemoved) != 1 || !strings.HasPrefix(r.MkRemoved[0], "device/google/tegu/Android.mk") {
		t.Errorf("removed: %v", r.MkRemoved)
	}
	// wrap2's own wrapper AND the real makefile beneath it are both denylisted and both reported.
	if !reflect.DeepEqual(r.BlockedMk, []string{"device/google/real/Android.mk", "device/google/wrap2/Android.mk", "device/google/wrap2/sub/Android.mk"}) {
		t.Errorf("a real module makefile and a wrapper with makefiles beneath it must stay blocked: %v", r.BlockedMk)
	}
}

func TestMaterializeSkipsVestigialWrapper(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{androidmkDenylistRel: fakeDenylistGo})
	delete(overlayRemovesCache, root)
	if _, err := materializeEmbedded("device/google/tegu", root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "device/google/tegu/Android.mk")); err == nil {
		t.Fatal("tegu's include-only root Android.mk must never land on a target that denylists it")
	}
	if _, err := os.Stat(filepath.Join(root, "device/google/tegu/aosp_tegu.mk")); err != nil {
		t.Fatal("the rest of tegu must land")
	}
}
