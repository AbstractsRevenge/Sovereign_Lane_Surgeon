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

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A self-extractor shaped like cheetah's, plus the device tree lines that include its glue.
func fixtureSelfExtractor(t *testing.T, root string) string {
	sx := filepath.Join(root, "device", "google", "fam", "self-extractors_dev")
	write(t, filepath.Join(root, "device", "google", "fam", "dev", "BoardConfig.mk"),
		"include device/google/gs201/BoardConfig-common.mk\n-include vendor/google_devices/gs201/prebuilts/BoardConfigVendor.mk\n-include vendor/google_devices/dev/proprietary/BoardConfigVendor.mk\n")
	write(t, filepath.Join(root, "device", "google", "fam", "device-dev.mk"),
		"$(call inherit-product-if-exists, vendor/google_devices/dev/proprietary/device-vendor.mk)\n")
	write(t, filepath.Join(sx, "extract-lists.txt"), `  google_devices)
    TO_EXTRACT="\
            IMAGES/vendor.img \
            RADIO/bootloader.img \
            system_ext/etc/permissions/com.shannon.imsservice.xml \
            system_ext/lib64/libmediaadaptor.so \
            system_ext/priv-app/ShannonIms/ShannonIms.apk \
            "
    ;;
`)
	write(t, filepath.Join(sx, "root", "proprietary", "BoardConfigVendor.mk"), "-include vendor/google_devices/dev/BoardConfigPartial.mk\n")
	write(t, filepath.Join(sx, "root", "proprietary", "device-vendor.mk"), "$(call inherit-product-if-exists, vendor/google_devices/dev/device-partial.mk)\n")
	write(t, filepath.Join(sx, "root", "android-info.txt"), "require board=dev\n")
	write(t, filepath.Join(sx, "google_devices", "LICENSE"), "license")
	write(t, filepath.Join(sx, "google_devices", "COPYRIGHT"), "copyright")
	write(t, filepath.Join(sx, "google_devices", "staging", "BoardConfigPartial.mk"), "BOARD_PREBUILT_VENDORIMAGE := vendor/google_devices/dev/proprietary/vendor.img\n")
	write(t, filepath.Join(sx, "google_devices", "staging", "device-partial.mk"), "PRODUCT_PACKAGES := ShannonIms libmediaadaptor\nPRODUCT_COPY_FILES := \\\n    vendor/google_devices/dev/proprietary/com.shannon.imsservice.xml:system_ext/etc/permissions/com.shannon.imsservice.xml:samsung \\\n")
	write(t, filepath.Join(sx, "google_devices", "staging", "Android.bp.txt"), "cc_prebuilt_library_shared {\n    name: \"libmediaadaptor\",\n    arch: { arm64: { srcs: [\"lib64/libmediaadaptor.so\"] } },\n}\n")
	write(t, filepath.Join(sx, "google_devices", "staging", "Android.mk.template"), "LOCAL_MODULE := ShannonIms\nLOCAL_SRC_FILES := $(LOCAL_MODULE).apk\n")
	return sx
}

func TestGlueDestinationIsReadFromTheIncludingTree(t *testing.T) {
	root := t.TempDir()
	sx := fixtureSelfExtractor(t, root)
	cases := map[string]string{
		filepath.Join(sx, "root", "proprietary", "BoardConfigVendor.mk"):        "proprietary/BoardConfigVendor.mk",
		filepath.Join(sx, "root", "proprietary", "device-vendor.mk"):            "proprietary/device-vendor.mk",
		filepath.Join(sx, "google_devices", "staging", "BoardConfigPartial.mk"): "BoardConfigPartial.mk",
		filepath.Join(sx, "google_devices", "staging", "device-partial.mk"):     "device-partial.mk",
	}
	for src, want := range cases {
		if got := filepath.ToSlash(glueDestination(root, "fam", "dev", src)); got != want {
			t.Errorf("%s: got %q want %q", filepath.Base(src), got, want)
		}
	}
	// No include anywhere: the self-extractor's own layout decides.
	bare := t.TempDir()
	if got := filepath.ToSlash(glueDestination(bare, "fam", "dev", filepath.Join(sx, "root", "proprietary", "BoardConfigVendor.mk"))); got != "proprietary/BoardConfigVendor.mk" {
		t.Errorf("fallback: got %q", got)
	}
}

func TestBlobDestinationFollowsTheConsumers(t *testing.T) {
	root := t.TempDir()
	sx := fixtureSelfExtractor(t, root)
	for base, want := range map[string]string{
		"libmediaadaptor.so":         "lib64/libmediaadaptor.so", // Android.bp.txt srcs
		"com.shannon.imsservice.xml": "com.shannon.imsservice.xml",
		"ShannonIms.apk":             "ShannonIms.apk",
		"vendor.img":                 "vendor.img",
	} {
		if got := filepath.ToSlash(blobDestination(sx, "dev", base)); got != want {
			t.Errorf("%s: got %q want %q", base, got, want)
		}
	}
}

// wireVendorBlobs: glue lands where the tree includes it, the staging module files become
// proprietary/Android.{mk,bp}, system_ext blobs are read from the unpacked image tree, and a
// flat BoardConfigVendor.mk from the earlier revision is moved (with USE_ANDROID_INFO kept).
func TestWireVendorBlobsPlacesGlueAndSystemExtBlobs(t *testing.T) {
	root := t.TempDir()
	fixtureSelfExtractor(t, root)
	factory := filepath.Join(root, "factory", "dev")
	write(t, filepath.Join(factory, "vendor.img"), "VENDOR")
	write(t, filepath.Join(factory, "dev-cp2a", "bootloader-dev-1.img"), "BL")
	write(t, filepath.Join(factory, "android-info.txt"), "require version-bootloader=x-17\n")
	vendorDir := filepath.Join(root, "vendor", "google_devices", "dev")
	// what extractVendorImages would have produced from system_ext.img
	write(t, filepath.Join(vendorDir, "system_ext", "etc", "permissions", "com.shannon.imsservice.xml"), "<xml/>")
	write(t, filepath.Join(vendorDir, "system_ext", "lib64", "libmediaadaptor.so"), "ELF")
	write(t, filepath.Join(vendorDir, "system_ext", "priv-app", "ShannonIms", "ShannonIms.apk"), "APK")
	// a flat copy from the earlier revision
	write(t, filepath.Join(vendorDir, "BoardConfigVendor.mk"), "USE_ANDROID_INFO := true\n\n-include vendor/google_devices/dev/BoardConfigPartial.mk\n")

	if err := wireVendorBlobs(root, "fam", "dev", factory); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"proprietary/BoardConfigVendor.mk", "proprietary/device-vendor.mk", "BoardConfigPartial.mk", "device-partial.mk",
		"proprietary/Android.mk", "proprietary/Android.bp", "proprietary/vendor.img", "proprietary/bootloader.img",
		"proprietary/com.shannon.imsservice.xml", "proprietary/lib64/libmediaadaptor.so", "proprietary/ShannonIms.apk", "android-info.txt", "LICENSE", "COPYRIGHT",
	} {
		if !fileExists(filepath.Join(vendorDir, filepath.FromSlash(rel))) {
			t.Errorf("missing %s", rel)
		}
	}
	if fileExists(filepath.Join(vendorDir, "BoardConfigVendor.mk")) {
		t.Error("flat BoardConfigVendor.mk should have been moved, not copied")
	}
	b, _ := os.ReadFile(filepath.Join(vendorDir, "proprietary", "BoardConfigVendor.mk"))
	if strings.Count(string(b), "USE_ANDROID_INFO := true") != 1 {
		t.Errorf("USE_ANDROID_INFO must be set exactly once:\n%s", b)
	}
	// idempotent
	if err := wireVendorBlobs(root, "fam", "dev", factory); err != nil {
		t.Fatal(err)
	}
}
