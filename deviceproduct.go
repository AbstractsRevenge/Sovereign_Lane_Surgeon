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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	parser "github.com/abstractsrevenge/sovereign_lane_surgeon/internal/blueprint/parser"
)

// deviceproduct.go — §23.1 step 5. Generates device/google/<family>-<lane>/ product makefiles
// for a sovereign lane. CRITICAL DISTINCTION (T-flagged 2026-07-12): the device-family FOLDER
// (e.g. pantah) is NOT the PRODUCT (e.g. cheetah, panther) — cheetah/panther are products inside
// device/google/pantah/. So the lane device dir is device/google/<family>-<lane>/ while the
// product is aosp_<product>_<lane>. Family + SoC are DERIVED from the tree (resolveDevice reads
// device/google/*/AndroidProducts.mk + the stock aosp_<product>.mk) — no guess table to go stale.
// LANE-UNIVERSAL lines mirror the proven device/google/lynx-holo/aosp_lynx_holo.mk (for lynx,
// family==product==lynx, so byte-identical); the SoC-common + device-HW inherits are auto-filled
// when resolved; remaining device-specific knobs (kernel, app-list, filter-outs) stay TODO.

type devTmplData struct {
	Lane         string // "holo"
	CamelCase    string // "Holo"
	Product      string // "cheetah" (PRODUCT_DEVICE / aosp_<product>_<lane>)
	ProductTitle string // "Cheetah"
	Family       string // "pantah" (device/google/ folder; == Product for lynx/tangorpro)
	SoC          string // "gs201" — auto-derived; "" ⇒ emits a TODO
	Rename       bool   // rename/app-naming model (DirPrefix set) — emits the keep-name-stub app allowlist
}

// deviceResolution is the tree-derived identity of a target device.
type deviceResolution struct {
	Product      string
	ProductTitle string
	Family       string
	SoC          string
	Resolved     bool // true when family/SoC were found in the tree
}

// resolveDevice derives the device family + SoC by reading the tree: which
// device/google/<family>/AndroidProducts.mk lists aosp_<product>.mk (→ family), and which
// device/google/<soc>/aosp_common.mk that stock product inherits (→ SoC). Falls back to
// family==product / SoC="" when the tree can't be read (dry-run, non-AOSP -out).
func resolveDevice(outRoot, product string) deviceResolution {
	res := deviceResolution{Product: product, ProductTitle: title(product), Family: product}
	if outRoot == "" {
		return res
	}
	matches, _ := filepath.Glob(filepath.Join(outRoot, "device", "google", "*", "AndroidProducts.mk"))
	needle := "aosp_" + product + ".mk"
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil || !strings.Contains(string(b), needle) {
			continue
		}
		res.Family = filepath.Base(filepath.Dir(m))
		stock := filepath.Join(filepath.Dir(m), needle)
		if sb, serr := os.ReadFile(stock); serr == nil {
			res.SoC = extractSoCInherit(string(sb))
			res.Resolved = true
		}
		break
	}
	return res
}

// extractSoCInherit finds the SoC-common dir in `inherit-product, device/google/<soc>/aosp_common.mk`
// (string-scan, no regex — HARD RULE 3 kept even for reads).
func extractSoCInherit(mk string) string {
	const pre = "device/google/"
	const suf = "/aosp_common.mk"
	for _, line := range strings.Split(mk, "\n") {
		i := strings.Index(line, pre)
		if i < 0 {
			continue
		}
		rest := line[i+len(pre):]
		if j := strings.Index(rest, suf); j > 0 && !strings.Contains(rest[:j], "/") {
			return rest[:j]
		}
	}
	return ""
}

// deviceProductTmpl is the aosp_<product>_<lane>.mk template. Ordering mirrors aosp_lynx_holo.mk.
var deviceProductTmpl = template.Must(template.New("devprod").Parse(`#
# Copyright 2026 The Android Open-Source Project
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

# {{.CamelCase}} lane product makefile for the {{.Product}} device (family device/google/{{.Family}}).
# Sovereign device-dir seed: lane content lives here + frameworks-{{.Lane}}/ + packages-{{.Lane}}/;
# stock material content stays in device/google/{{.Family}}/. Hardware glue (audio, bluetooth, board
# config, display cal) is shared chassis infrastructure referenced via stock paths in the included
# subdir mks — sovereignty is scoped to lane CONTENT, not duplicating chassis hardware files.
#
# Lunch with explicit release: ` + "`lunch aosp_{{.Product}}_{{.Lane}}-<release>-eng`" + `.

# TODO({{.Product}}): device/SoC hardware knobs — copy from the stock device/google/{{.Family}}/ product.
# TARGET_LINUX_KERNEL_VERSION := <ver>
# DEVICE_USES_NO_TRUSTY := true
# USE_SWIFTSHADER := true
# BOARD_USES_SWIFTSHADER := true

{{if .SoC}}# SoC-common (auto-derived from the stock aosp_{{.Product}}.mk).
$(call inherit-product, device/google/{{.SoC}}/aosp_common.mk){{else}}# TODO({{.Product}}): inherit the SoC-common product (could not auto-derive from the tree), e.g.:
# $(call inherit-product, device/google/<soc>/aosp_common.mk){{end}}
$(call inherit-product, device/google/{{.Family}}-{{.Lane}}/device-{{.Product}}_{{.Lane}}.mk)

# {{.CamelCase}} Platform Key — the lane's sovereign signing identity. Until you generate it, the
# seed uses the inherited AOSP testkey so the lane BUILDS out of the box. TODO({{.Lane}}): generate
# the key (development/tools/make_key build/make/target/product/security/{{.Lane}}/{{.Lane}}_platform
# '/CN=Android/...'), then UNCOMMENT the line below so every certificate:"platform" module signs with it.
# PRODUCT_DEFAULT_DEV_CERTIFICATE := build/make/target/product/security/{{.Lane}}/{{.Lane}}_platform

# bootable/deprecated-ota carries the updater's static libs (libupdater_device/core, libapplypatch,
# libedify); without this namespace a lane build fails kati with "updater missing STATIC_LIBRARIES".
PRODUCT_SOONG_NAMESPACES += \
    bootable/deprecated-ota \
    frameworks-{{.Lane}} \
    packages-{{.Lane}}
# TODO({{.Lane}}): add the lane's specific sub-namespaces (whatever subtrees you forked), e.g.
#   frameworks-{{.Lane}}/base  frameworks-{{.Lane}}/base/packages  frameworks-{{.Lane}}/native  packages-{{.Lane}}/wallpapers

SOONG_CONFIG_NAMESPACES += {{.Lane}}_framework_routing {{.Lane}}_package_routing
SOONG_CONFIG_{{.Lane}}_framework_routing += enable_{{.Lane}}_java_overrides
SOONG_CONFIG_{{.Lane}}_framework_routing_enable_{{.Lane}}_java_overrides := true
SOONG_CONFIG_{{.Lane}}_framework_routing += enable_{{.Lane}}_res
SOONG_CONFIG_{{.Lane}}_framework_routing_enable_{{.Lane}}_res := true
SOONG_CONFIG_{{.Lane}}_framework_routing += enable_{{.Lane}}_kotlinc
SOONG_CONFIG_{{.Lane}}_framework_routing_enable_{{.Lane}}_kotlinc := true
SOONG_CONFIG_{{.Lane}}_package_routing += enable_{{.Lane}}_package_overrides
SOONG_CONFIG_{{.Lane}}_package_routing_enable_{{.Lane}}_package_overrides := true

# TODO({{.Lane}}): the lane app surface. Prefixed installables ({{.CamelCase}}<Name>, each declaring
# overrides:["<stock>"] in its bp to suppress the stock variant), plus lane-only apps. Example:
#   PRODUCT_PACKAGES += \
#       {{.CamelCase}}Camera {{.CamelCase}}Contacts {{.CamelCase}}Dialer {{.CamelCase}}DeskClock {{.CamelCase}}Settings ...

# TODO({{.Lane}}): filter-out any stock Google/unwanted modules the lane replaces (SettingsGoogle,
# SystemUIGoogle, EasterEgg, the stock sound wrappers, stray JNI libs). Place this filter-out
# where inherit-product has already populated PRODUCT_PACKAGES — a filter-out too early is a no-op.

# {{.CamelCase}} lane framework-res variant is declared in frameworks-{{.Lane}}/base/core/res/Android.bp;
# the aosp_{{.Product}}_{{.Lane}}-* lunch makes build/soong/java/aar.go suppress the stock framework-res's
# AndroidMkEntries so the lane variant alone installs at system/framework/framework-res.apk. No
# PRODUCT_PACKAGES entry / install-overlay hook is needed — the lunch inherently scopes it.

# {{.CamelCase}}*Google replacement apps install at their own /<Name>/ paths; allow them past the
# GSI artifact-path-requirement check — they're the lane's first-class apps.
PRODUCT_ARTIFACT_PATH_REQUIREMENT_ALLOWED_LIST += \
    system/app/{{.CamelCase}}% \
    system/priv-app/{{.CamelCase}}% \
    system_ext/app/{{.CamelCase}}% \
    system_ext/priv-app/{{.CamelCase}}% \
    product/app/{{.CamelCase}}% \
    product/priv-app/{{.CamelCase}}% \
    product/overlay/{{.CamelCase}}%
{{if .Rename}}
# Rename model only: re-export keep-name app stubs (frameworks-{{.Lane}}/base/shared-app-defaults) exist
# for graph coherence with the generic_system MODULE (which names apps like Settings/Browser2 by keep-name
# via PRODUCT_PACKAGES). Under the Holo-style full-collapse they install at keep-name system/app/<Name>/
# paths, inside generic_system.mk's artifact_path_requirement but not its narrow allowed set. Permit the
# lane app footprint, mirroring lynx-holo's system/app/Holo% allowance.
PRODUCT_ARTIFACT_PATH_REQUIREMENT_ALLOWED_LIST += \
    system/app/% \
    system/priv-app/% \
    system_ext/app/% \
    system_ext/priv-app/% \
    product/app/% \
    product/priv-app/%
{{end}}
# The lane ships an expanded font family + demo videos beyond the narrow Material set
# generic_system.mk's allowlist permits — allow the broader footprint here.
PRODUCT_ARTIFACT_PATH_REQUIREMENT_ALLOWED_LIST += \
    system/fonts/% \
    system/etc/fallback_fonts.xml \
    system/etc/system_fonts.xml \
    system/etc/vendor_fonts.xml \
    system/media/video/%

# Privileged-app permission XMLs install to /system, /product, /system_ext etc/permissions/;
# allow-list the privapp-permissions-google* set past the artifact-path check.
PRODUCT_ARTIFACT_PATH_REQUIREMENT_ALLOWED_LIST += \
    system/etc/permissions/privapp-permissions-google.xml \
    system/etc/permissions/privapp-permissions-google-%.xml \
    product/etc/permissions/privapp-permissions-google-%.xml \
    system_ext/etc/permissions/privapp-permissions-google-%.xml

$(call inherit-product-if-exists, packages-{{.Lane}}/bootanimation/phone/bootanimation.mk)


PRODUCT_NAME := aosp_{{.Product}}_{{.Lane}}
PRODUCT_DEVICE := {{.Product}}
PRODUCT_MODEL := AOSP {{.CamelCase}} on {{.ProductTitle}}
PRODUCT_BRAND := Android
PRODUCT_MANUFACTURER := Google

DEVICE_MANIFEST_FILE := device/google/{{.Family}}-{{.Lane}}/manifest.xml
PRODUCT_CHARACTERISTICS += {{.Lane}}
PRODUCT_SYSTEM_PROPERTIES += ro.{{.Lane}}.framework=true

# Lane identity sound defaults — land in PRODUCT_PRODUCT_PROPERTIES so they write LAST into
# /product/etc/build.prop (loaded last on this device) and win at runtime over the inherited
# aosp_product.mk defaults. TODO({{.Lane}}): pick the lane's ringtone/notification/alarm.
PRODUCT_PRODUCT_PROPERTIES += \
    ro.config.ringtone=Pegasus.ogg \
    ro.config.notification_sound=Aldebaran.ogg \
    ro.config.alarm_alert=Cesium.ogg

# Lane-sourced audio install: {{.CamelCase}}Audio.mk adds the lane-only sound set. The upstream
# AllAudio.mk PRODUCT_COPY_FILES entries are stripped by a deferred filter-out — it must run
# AFTER all inherit-product calls resolve (kati lazy eval), so keep it near end-of-file.
# TODO({{.Lane}}): PRODUCT_COPY_FILES := $(filter-out frameworks/base/data/sounds/%,$(PRODUCT_COPY_FILES))
$(call inherit-product-if-exists, frameworks-{{.Lane}}/base/data/sounds/{{.CamelCase}}Audio.mk)

# Lane-sourced font install: replace the upstream Material font set with the lane family under
# frameworks-{{.Lane}}/base/data/fonts/. TODO({{.Lane}}): filter out the upstream prebuilt_font
# modules (Roboto-Regular.ttf, DroidSansMono.ttf, AndroidClock.ttf, fonts.xml, ...) first.
$(call inherit-product-if-exists, frameworks-{{.Lane}}/base/data/fonts/{{.CamelCase}}Fonts.mk)

# Lane-sourced demo videos at /system/media/video/ (modern AOSP ships none, so no filter needed).
$(call inherit-product-if-exists, frameworks-{{.Lane}}/base/data/videos/{{.CamelCase}}Video.mk)

# Privileged permission allowlist for re-signed / source-built lane SystemUI + Settings.
PRODUCT_PACKAGES += \
    privapp_whitelist_com.android.systemui \
    privapp_whitelist_com.android.settings

# Pixel-extracted privapp permission XMLs: NOT copied inline. An inline PRODUCT_COPY_FILES of
# device/google/{{.Family}}-{{.Lane}}/privapp_permissions/privapp-permissions-google*.xml fails the
# build at ninja ("missing and no known rule to make it") because a fresh device scaffold has no such
# XMLs, and even once present it duplicate-dest-collides with the shared lane rule. The proven sibling
# removed this exact block: the standard privapp_whitelist_com.android.{systemui,settings} modules
# above cover the re-signed apps' perms, and packages-{{.Lane}}/{{.Lane}}_apps.mk (inherited once
# first-class lane Settings/SystemUI land) single-sources privapp-permissions-{{.Lane}}.xml for every
# device. TODO({{.Lane}}): inherit that shared rule when the lane's priv-apps land — do NOT revive the
# inline copy.
`))

// androidProductsMkTmpl — device AndroidProducts.mk (one product entry + 3 lunch choices).
var androidProductsMkTmpl = template.Must(template.New("apm").Parse(`#
# Copyright (C) 2026 The Android Open-Source Project
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

PRODUCT_MAKEFILES := \
    $(LOCAL_DIR)/aosp_{{.Product}}_{{.Lane}}.mk

COMMON_LUNCH_CHOICES := \
    aosp_{{.Product}}_{{.Lane}}-bp1a-userdebug \
    aosp_{{.Product}}_{{.Lane}}-bp1a-user \
    aosp_{{.Product}}_{{.Lane}}-bp1a-eng
`))

// deviceAndroidBpTmpl — the device dir's Android.bp (package + license boilerplate). The license
// name keys on the FAMILY dir (device_google_<family>_<lane>).
var deviceAndroidBpTmpl = template.Must(template.New("dbp").Parse(`package {
    default_applicable_licenses: ["device_google_{{.Family}}_{{.Lane}}_license"],
}

license {
    name: "device_google_{{.Family}}_{{.Lane}}_license",
    visibility: [":__subpackages__"],
    license_kinds: ["SPDX-license-identifier-Apache-2.0"],
    license_text: ["NOTICE"],
}
`))

// deviceMkStubTmpl — device-<product>_<lane>.mk. Only the LANE-DELTA lines are stubbed; the
// hardware body (vendor inherits, SoC device-shipping-common, audio/nfc/thermal copies) is the
// device owner's to bring over from the stock device/google/<family>/device-<product>.mk.
var deviceMkStubTmpl = template.Must(template.New("dmk").Parse(`#
# {{.CamelCase}} lane device makefile for {{.Product}} (family {{.Family}}). Inherits the STOCK
# device HW body (vendor blobs, HALs, kernel, audio/nfc/wifi/thermal — shared chassis
# infrastructure; sovereignty is scoped to lane CONTENT, not HW). Add lane device deltas below.
#
{{if .SoC}}$(call inherit-product, device/google/{{.Family}}/device-{{.Product}}.mk)
{{else}}# TODO({{.Product}}): inherit the stock device HW body (could not auto-derive):
# $(call inherit-product, device/google/{{.Family}}/device-{{.Product}}.mk)
{{end}}
# --- lane deltas (uncomment/extend once frameworks-{{.Lane}}/overlays exists) ---
# DEVICE_PACKAGE_OVERLAYS += frameworks-{{.Lane}}/overlays
# PRODUCT_ENFORCE_RRO_EXCLUDED_OVERLAYS += frameworks-{{.Lane}}/overlays
# TODO({{.Lane}}): lane overlay/launcher packages, e.g. PRODUCT_PACKAGES += {{.CamelCase}}OverlayLauncher3
`))

// genDeviceProduct renders one device file for a resolved device + returns (rel-path, content).
func genDeviceProduct(c LaneConfig, res deviceResolution, tmpl *template.Template, fname string) (string, string, error) {
	data := devTmplData{
		Lane: c.Name, CamelCase: c.CamelCase,
		Product: res.Product, ProductTitle: res.ProductTitle,
		Family: res.Family, SoC: res.SoC,
		Rename: c.DirPrefix != "",
	}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		return "", "", err
	}
	rel := filepath.Join("device", "google", res.Family+"-"+c.Name, fname)
	return rel, sb.String(), nil
}

// title upper-cases the first rune (lynx → Lynx) without importing strings.Title (deprecated).
func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// writeDeviceProducts generates device/google/<family>-<lane>/ for each target device, resolving
// family + SoC from the tree first and reporting the resolution to the user. Before the lane-content
// files, it COPIES the full stock device/google/<family>/ tree into the lane dir (once per family)
// so the lane device dir is COMPLETE + self-contained (all HW files + subdirs), per the proven
// lynx-nexusm model — see copyDeviceFamilyTree.
func writeDeviceProducts(c LaneConfig, outRoot string) (wrote, skipped int, fatal bool) {
	copiedFamilies := map[string]bool{}
	for _, product := range c.Devices {
		res := resolveDevice(outRoot, product)
		if res.Resolved {
			fmt.Printf("  device %s → family=%s soc=%s → device/google/%s-%s/\n", product, res.Family, res.SoC, res.Family, c.Name)
		} else {
			fmt.Printf("  device %s → UNRESOLVED (family assumed %q, SoC left TODO — not found under -out)\n", product, res.Family)
		}
		// Copy the full stock family HW tree once per family (cheetah+panther share pantah). The
		// per-product lane mks (below) then land ON TOP via writeIfAbsent.
		if outRoot != "" && !copiedFamilies[res.Family] {
			copiedFamilies[res.Family] = true
			cp, dr, cerr := copyDeviceFamilyTree(c, outRoot, res.Family)
			if cerr != nil {
				fmt.Fprintf(os.Stderr, "  ! copy device family tree %s: %v\n", res.Family, cerr)
			} else if cp > 0 || dr > 0 {
				fmt.Printf("  copied %d HW file(s) from device/google/%s/ → device/google/%s-%s/ (dropped %d collision-prone overlay .bp)\n", cp, res.Family, res.Family, c.Name, dr)
				wrote += cp
			}
		}
		files := []struct {
			tmpl  *template.Template
			fname string
		}{
			{deviceProductTmpl, fmt.Sprintf("aosp_%s_%s.mk", res.Product, c.Name)},
			{androidProductsMkTmpl, "AndroidProducts.mk"},
			{deviceAndroidBpTmpl, "Android.bp"},
			{deviceMkStubTmpl, fmt.Sprintf("device-%s_%s.mk", res.Product, c.Name)},
		}
		for _, f := range files {
			rel, content, err := genDeviceProduct(c, res, f.tmpl, f.fname)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  ! render device %s: %v\n", product, err)
				return wrote, skipped, true
			}
			w, s := writeIfAbsent(outRoot, rel, content)
			wrote += w
			skipped += s
		}
		// NOTICE for the device Android.bp license module (license_text: ["NOTICE"]).
		w, s := writeIfAbsent(outRoot, filepath.Join("device", "google", res.Family+"-"+c.Name, "NOTICE"), apacheNotice)
		wrote += w
		skipped += s
		// Multi-product families (cheetah + panther both in pantah-<lane>): the AndroidProducts.mk
		// template above is writeIfAbsent, so the 2nd+ product would be skipped and never register.
		// Merge this product into the existing list instead.
		switch registerDeviceProduct(c, outRoot, res) {
		case patchApplied:
			fmt.Printf("  + registered %s in device/google/%s-%s/AndroidProducts.mk (merged; snapshot .sld-bak)\n", res.Product, res.Family, c.Name)
		case patchUnrecognized:
			fmt.Fprintf(os.Stderr, "  ! could not merge %s into AndroidProducts.mk (no PRODUCT_MAKEFILES continuation list) — add manually\n", res.Product)
		}
	}
	return wrote, skipped, false
}

// registerDeviceProduct merges a product into an EXISTING device-family AndroidProducts.mk (the
// multi-product case: pantah-<lane> already carries panther, now add cheetah). Inserts the product
// mk line into PRODUCT_MAKEFILES + the three bp1a lunch choices into COMMON_LUNCH_CHOICES via
// insertAfterListHead. Idempotent (patchAlreadyDone if the product mk is already listed — including
// the freshly-templated single-product file, where it's a no-op), snapshotted (.sld-bak).
func registerDeviceProduct(c LaneConfig, outRoot string, res deviceResolution) patchStatus {
	abs := filepath.Join(outRoot, "device", "google", res.Family+"-"+c.Name, "AndroidProducts.mk")
	raw, err := os.ReadFile(abs)
	if err != nil {
		return patchFileMissing
	}
	prodMk := fmt.Sprintf("aosp_%s_%s.mk", res.Product, c.Name)
	if strings.Contains(string(raw), prodMk) {
		return patchAlreadyDone
	}
	prod := fmt.Sprintf("aosp_%s_%s", res.Product, c.Name)
	mkEntries := []string{fmt.Sprintf("    $(LOCAL_DIR)/%s \\", prodMk)}
	lunchEntries := []string{
		fmt.Sprintf("    %s-bp1a-userdebug \\", prod),
		fmt.Sprintf("    %s-bp1a-user \\", prod),
		fmt.Sprintf("    %s-bp1a-eng \\", prod),
	}
	out := insertAfterListHead(strings.Split(string(raw), "\n"), "PRODUCT_MAKEFILES", mkEntries)
	out = insertAfterListHead(out, "COMMON_LUNCH_CHOICES", lunchEntries)
	if out == nil {
		return patchUnrecognized
	}
	if bak := abs + ".sld-bak"; func() bool { _, e := os.Stat(bak); return os.IsNotExist(e) }() {
		_ = os.WriteFile(abs+".sld-bak", raw, 0o644)
	}
	if os.WriteFile(abs, []byte(strings.Join(out, "\n")), 0o644) != nil {
		return patchUnrecognized
	}
	return patchApplied
}

// bpCollisionProneTypes are the module types a copied device .bp must NOT redefine under a
// stock name — installables + runtime resource overlays whose names are GLOBAL in Soong, so a
// verbatim copy would "module already defined" against the stock dir. The proven lynx-nexusm
// model DROPS these (the lane supplies its own overlays); HW-infra .bp (cc_*, prebuilt_*,
// powerstats) keep their names and are relicensed. NexusM's app-prefixing is a SEPARATE concern
// that lives in packages-<lane>, NOT here — a device HW module is not an app.
var bpCollisionProneTypes = map[string]bool{
	"runtime_resource_overlay":          true,
	"override_runtime_resource_overlay": true,
	"android_app":                       true,
	"android_app_import":                true,
	"override_android_app":              true,
}

// pathHasOverlaySegment reports whether any path segment contains "overlay" (case-insensitive) —
// the marker for a device-dir overlay subtree: overlay_packages/, rro_overlays/, overlay-holo/
// (ANOTHER lane's overlay), overlay-product-*/. These declare overlays OR bare soong_namespace
// scopes the lane replaces with its own; their .bp is dropped (resources are still copied, matching
// lynx-nexusm which dropped only the .bp). powerstats/ etc. have no "overlay" segment → kept.
func pathHasOverlaySegment(rel string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.Contains(strings.ToLower(seg), "overlay") {
			return true
		}
	}
	return false
}

// bpIsCollisionProne parses a device .bp (AST, not text — HARD RULE 3) and reports whether it
// declares any collision-prone module (overlay/installable). Such a .bp is dropped from the copy.
// A parse failure is treated as collision-prone=false (copy + relicense) so we never silently drop
// a HW .bp on a lexer hiccup; the build is the backstop.
func bpIsCollisionProne(content []byte) bool {
	file, errs := parser.Parse("", bytes.NewReader(content))
	if len(errs) > 0 {
		return false
	}
	for _, def := range file.Defs {
		if m, ok := def.(*parser.Module); ok && bpCollisionProneTypes[m.Type] {
			return true
		}
	}
	return false
}

// isStockProductMk reports whether a root-level device-dir file is a STOCK product/factory makefile
// that the lane REPLACES with its own aosp_<product>_<lane>.mk / device-<product>_<lane>.mk (so it
// must be excluded from the copy). Matches aosp_*.mk, factory_*.mk, device-*.mk, device_*.mk, and
// AndroidProducts.mk — all the product-defining mks at the family root. Sub-dir .mk (audio-tables,
// etc.) are HW and are NOT matched (this only fires at depth 0).
func isStockProductMk(base string) bool {
	if !strings.HasSuffix(base, ".mk") {
		return false
	}
	if base == "AndroidProducts.mk" {
		return true
	}
	return strings.HasPrefix(base, "aosp_") || strings.HasPrefix(base, "factory_") ||
		strings.HasPrefix(base, "device-") || strings.HasPrefix(base, "device_")
}

// copyDeviceFamilyTree copies the full stock device/google/<family>/ tree into the lane device dir
// device/google/<family>-<lane>/, making it a COMPLETE, self-contained device directory (all HW
// files + subdirs), per the proven lynx-nexusm model. Option A:
//   - EXCLUDE .git/.repo; the stock product mks at the family root (isStockProductMk); and the root
//     Android.bp + NOTICE (the caller's templates emit the lane-licensed versions).
//   - DROP collision-prone overlay/installable .bp (bpIsCollisionProne) — the lane supplies its own.
//   - KEEP + RELICENSE HW .bp (rewrite device_google_<family>_license → _<lane>_license so the ref
//     resolves against the lane license module the root Android.bp template declares).
//   - COPY everything else verbatim (preserve symlinks, no-clobber so a re-run / hand-tuned file wins).
//
// Returns (files copied, .bp dropped, error).
func copyDeviceFamilyTree(c LaneConfig, outRoot, family string) (copied, dropped int, err error) {
	src := filepath.Join(outRoot, "device", "google", family)
	dstRoot := filepath.Join(outRoot, "device", "google", family+"-"+c.Name)
	info, serr := os.Stat(src)
	if serr != nil || !info.IsDir() {
		return 0, 0, fmt.Errorf("stock device dir device/google/%s not found under -out", family)
	}
	oldLic := "device_google_" + family + "_license"
	newLic := "device_google_" + family + "_" + c.Name + "_license"
	walkErr := filepath.Walk(src, func(p string, fi os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if base := filepath.Base(p); base == ".git" || base == ".repo" {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		base := filepath.Base(rel)
		atRoot := !strings.Contains(rel, string(filepath.Separator))
		// Root-level exclusions: stock product mks + the root Android.bp/NOTICE (templates emit the
		// lane-licensed versions).
		if atRoot && (isStockProductMk(base) || base == "Android.bp" || base == "NOTICE") {
			return nil
		}
		// BOARD subdirs are excluded — they must stay unique per TARGET_DEVICE.
		//
		// board_config.mk locates the board by GLOB, not by product path:
		//   find -L device -maxdepth 4 -path '*/$(TARGET_DEVICE)/BoardConfig.mk'
		// and the lane's product keeps the stock PRODUCT_DEVICE (`panther`, not `panther_<lane>`).
		// So a copied device/google/<family>-<lane>/panther/BoardConfig.mk matches that glob for
		// EVERY panther product — stock and lane alike — and kati aborts with
		// "Multiple board config files for TARGET_DEVICE panther". The lane shares the stock board;
		// only the product mks are lane-specific.
		if fi.IsDir() {
			if _, berr := os.Stat(filepath.Join(p, "BoardConfig.mk")); berr == nil {
				return filepath.SkipDir
			}
		}
		target := filepath.Join(dstRoot, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			link, lerr := os.Readlink(p)
			if lerr != nil {
				return nil
			}
			if _, lstatErr := os.Lstat(target); lstatErr == nil {
				return nil // no-clobber
			}
			if merr := os.MkdirAll(filepath.Dir(target), 0o755); merr != nil {
				return merr
			}
			if os.Symlink(link, target) == nil {
				copied++
			}
			return nil
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		if _, statErr := os.Stat(target); statErr == nil {
			return nil // no-clobber
		}
		// .bp handling: drop collision-prone overlay/installable bp; keep+relicense HW bp.
		if base == "Android.bp" {
			content, readErr := os.ReadFile(p)
			if readErr != nil {
				return nil
			}
			if pathHasOverlaySegment(rel) || bpIsCollisionProne(content) {
				dropped++
				return nil
			}
			relicensed := bytes.ReplaceAll(content, []byte(oldLic), []byte(newLic))
			if merr := os.MkdirAll(filepath.Dir(target), 0o755); merr != nil {
				return merr
			}
			if werr := os.WriteFile(target, relicensed, 0o644); werr != nil {
				return werr
			}
			copied++
			return nil
		}
		if cerr := copyFile(p, target); cerr != nil {
			return cerr
		}
		copied++
		return nil
	})
	return copied, dropped, walkErr
}

// apacheNotice is the standard Apache-2.0 NOTICE the device license module points at.
const apacheNotice = `   Copyright (C) 2026 The Android Open Source Project

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

        http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
`
