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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// scaffold.go — the `create` file-generation core (§23.1). v2 increment: the goldfish
// emulator products. Each generated file is a working sovereign-lane skeleton whose
// UNIVERSAL structure is filled in (the _<lane> PRODUCT_NAME suffix that engages the
// finder routing, the SOONG_CONFIG opt-in, the partition setup) and whose LANE-SPECIFIC
// slots are marked TODO with the §22 lesson that governs them (the boot-deadlock rule,
// the artifact-path exemption, the filter-out no-op trap). The tool does the mechanical
// 80%; the lane owner fills the 20% the tool cannot know — and learns why from the comment.

// emuForm captures the phone/tablet deltas over the shared goldfish emu product body.
type emuForm struct {
	Key      string // "phone" / "tablet" — inherited device/generic/goldfish/product/<Key>.mk
	Title    string // "Phone" / "Tablet" — PRODUCT_MODEL text
	ProdStem string // "sdk_phone64" / "sdk_tablet" — filename stem + PRODUCT_NAME stem
	SysImg   string // PRODUCT_SDK_ADDON_SYS_IMG_SOURCE_PROP template path
}

var emuForms = []emuForm{
	{
		Key: "phone", Title: "Phone", ProdStem: "sdk_phone64",
		SysImg: "development/sys-img/images_x86_64_source.prop_template",
	},
	{
		Key: "tablet", Title: "Tablet", ProdStem: "sdk_tablet",
		SysImg: "device/generic/goldfish/64bitonly/product/tablet_images_x86_64_source.prop_template",
	},
}

// emuTmplData is the flattened template context (LaneConfig + the active emuForm).
type emuTmplData struct {
	Name        string // lane, lowercase — "holo"
	CamelCase   string // "Holo"
	Form        emuForm
	ProductName string // "sdk_phone64_holo"
}

// emuProductTmpl mirrors the hand-verified sdk_<form>64_<lane>.mk. The one load-bearing
// line is PRODUCT_NAME := {{.ProductName}} — its _<lane> suffix is what is<Lane>Product()
// in build/soong/ui/build/finder.go keys on to drop the stock sovereign Android.bp paths.
var emuProductTmpl = template.Must(template.New("emu").Parse(`#
# Copyright (C) 2026 The Android Open Source Project
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
# {{.CamelCase}} lane EMULATOR ({{.Form.Title}}): Android SDK {{.Form.Title}} built on the
# {{.CamelCase}} lane. Body mirrors {{.Form.ProdStem}}_x86_64.mk (goldfish/ranchu virtual HW).
# The ONE load-bearing difference is PRODUCT_NAME := {{.ProductName}}: the "_{{.Name}}"
# suffix makes is{{.CamelCase}}Product() true in build/soong/ui/build/finder.go, so the
# finder drops the stock/other-lane sovereign Android.bp paths (per the .{{.Name}} route
# manifest) and the name-preserving {{.CamelCase}} framework/SystemUI modules win — the
# same routing every aosp_*_{{.Name}} device product gets. A distinct product => a distinct
# OUT_DIR => no Soong-lock contention with the physical-device {{.CamelCase}} lanes.
#
PRODUCT_USE_DYNAMIC_PARTITIONS := true
EMULATOR_DISABLE_RADIO := true

BOARD_EMULATOR_DYNAMIC_PARTITIONS_SIZE ?= $(shell expr 1800 \* 1048576 )
BOARD_SUPER_PARTITION_SIZE := $(shell expr $(BOARD_EMULATOR_DYNAMIC_PARTITIONS_SIZE) + 8388608 )  # +8M

$(call inherit-product, $(SRC_TARGET_DIR)/product/core_64_bit_only.mk)

PRODUCT_ENFORCE_ARTIFACT_PATH_REQUIREMENTS := relaxed

PRODUCT_SDK_ADDON_SYS_IMG_SOURCE_PROP := \
    {{.Form.SysImg}}

$(call inherit-product, device/generic/goldfish/board/emu64x/details.mk)
$(call inherit-product, device/generic/goldfish/product/{{.Form.Key}}.mk)

PRODUCT_BRAND := Android
PRODUCT_NAME := {{.ProductName}}
PRODUCT_DEVICE := emu64x
PRODUCT_MODEL := {{.CamelCase}} on {{.Form.Title}} (emulator)

# --- {{.CamelCase}} lane soong-config opt-in. The _{{.Name}} PRODUCT_NAME suffix engages the
# finder BP routing on its own; these per-product bools turn on the REST of the lane: the
# java/res/package overrides and (critically) the lane kotlinc that can read the lane AARs'
# Kotlin metadata. Keep these identical to what your device product aosp_<dev>_{{.Name}}.mk
# sets — the emu and the device must opt into the same routing or they diverge.
SOONG_CONFIG_NAMESPACES += {{.Name}}_framework_routing {{.Name}}_package_routing
SOONG_CONFIG_{{.Name}}_framework_routing += enable_{{.Name}}_java_overrides
SOONG_CONFIG_{{.Name}}_framework_routing_enable_{{.Name}}_java_overrides := true
SOONG_CONFIG_{{.Name}}_framework_routing += enable_{{.Name}}_res
SOONG_CONFIG_{{.Name}}_framework_routing_enable_{{.Name}}_res := true
SOONG_CONFIG_{{.Name}}_framework_routing += enable_{{.Name}}_kotlinc
SOONG_CONFIG_{{.Name}}_framework_routing_enable_{{.Name}}_kotlinc := true
SOONG_CONFIG_{{.Name}}_package_routing += enable_{{.Name}}_package_overrides
SOONG_CONFIG_{{.Name}}_package_routing_enable_{{.Name}}_package_overrides := true

# --- TODO({{.Name}}): drop any STOCK apps your lane replaces natively (redundant, and often
# broken under the lane toolchain). Example (the Holo lane dropped the Material-You ThemePicker):
#   PRODUCT_PACKAGES := $(filter-out ThemePicker,$(PRODUCT_PACKAGES))
# §22 TRAP: a filter-out here can be a silent NO-OP when inherit-product defers PRODUCT_PACKAGES
# flattening past this line. If it doesn't take, disable the module at its source instead
# (enabled: false in its Android.bp) and let ALLOW_MISSING_DEPENDENCIES=true absorb the dangle.

# --- {{.CamelCase}} app suite (REQUIRED for boot + UX). §22 Group F boot-deadlock: if your lane
# RENAMES its apps (e.g. {{.CamelCase}}Settings overrides Settings), the sovereign finder drops
# the stock <Name> bp, so the base product's inherited "PRODUCT_PACKAGES += <Name>" resolves to a
# DROPPED module and {{.CamelCase}}<Name> is never built. Without the lane's Settings there is no
# com.android.settings => no FallbackHome => a circular finishBooting/unlock BOOT DEADLOCK. You
# MUST explicitly request the renamed boot-essential apps here. Also install a launcher (HOME) or
# the emu boots to a bare FallbackHome. Keep every entry on an EXEMPT partition (system_ext or
# product): a renamed app in system/priv-app violates the emu's GSI generic_system.mk
# artifact-path-requirement (add PRODUCT_ARTIFACT_PATH_REQUIREMENT_ALLOWED_LIST if you need one).
# TODO({{.Name}}): list your lane's boot-essential renamed apps + a launcher, e.g.:
#   PRODUCT_PACKAGES += \
#       {{.CamelCase}}Settings \
#       Launcher3
`))

// genEmuProduct renders one goldfish emu product file: its repo-relative path + content.
// genEmuProductFromLane derives the emulator product from the SOURCE lane's own product instead
// of the TODO template. For a lane-sourced fork this is strictly better: the source lane's product
// already encodes the choices the template can only leave as TODOs — which apps the lane must ship
// (a lane that omits its Dialer/Contacts has no default provider and crash-loops on boot), the
// PRODUCT_SOONG_NAMESPACES it needs (bootable/deprecated-ota is not optional; without it kati
// fails on missing updater libs), the privileged-permission mode, and the artifact-path
// allowlist. Every lane token is repointed; returns ok=false if the source product is absent, so
// the caller falls back to the template.
func genEmuProductFromLane(c LaneConfig, f emuForm, outRoot, srcLane string) (rel, content string, ok bool) {
	srcName := f.ProdStem + "_" + srcLane
	newName := f.ProdStem + "_" + c.Name
	srcRel := filepath.Join("device", "generic", "goldfish", "64bitonly", "product", srcName+".mk")
	b, err := os.ReadFile(filepath.Join(outRoot, srcRel))
	if err != nil {
		return "", "", false
	}
	out := string(b)
	// Whole-token replacements, longest-first so <stem>_<lane> is rewritten before a bare <lane>.
	for _, r := range [][2]string{
		{srcName, newName},
		{"frameworks-" + srcLane, "frameworks-" + c.Name},
		{"packages-" + srcLane, "packages-" + c.Name},
		{srcLane + "_framework_routing", c.Name + "_framework_routing"},
		{srcLane + "_package_routing", c.Name + "_package_routing"},
		{"enable_" + srcLane + "_", "enable_" + c.Name + "_"},
	} {
		out = strings.ReplaceAll(out, r[0], r[1])
	}
	out = "# Derived by sovereign-lane-surgeon from " + srcRel + " (create -from " + srcLane + ").\n" +
		"# Lane tokens repointed; the app suite, namespaces and privapp mode are INHERITED, because\n" +
		"# those are exactly the fields the from-scratch template can only leave as TODOs.\n" + out
	return filepath.Join("device", "generic", "goldfish", "64bitonly", "product", newName+".mk"), out, true
}

func genEmuProduct(c LaneConfig, f emuForm) (string, string, error) {
	data := emuTmplData{
		Name:        c.Name,
		CamelCase:   c.CamelCase,
		Form:        f,
		ProductName: f.ProdStem + "_" + c.Name,
	}
	var sb strings.Builder
	if err := emuProductTmpl.Execute(&sb, data); err != nil {
		return "", "", err
	}
	rel := filepath.Join("device", "generic", "goldfish", "64bitonly", "product", data.ProductName+".mk")
	return rel, sb.String(), nil
}

// writeIfAbsent writes content to outRoot/rel, never clobbering an existing file (skip — a
// hand-tuned lane file is authored once, Rule 7). Returns (wrote, skipped) as 0/1 deltas.
func writeIfAbsent(outRoot, rel, content string) (wrote, skipped int) {
	abs := filepath.Join(outRoot, rel)
	if _, err := os.Stat(abs); err == nil {
		fmt.Printf("  = skip (exists): %s\n", rel)
		return 0, 1
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "  ! mkdir %s: %v\n", filepath.Dir(rel), err)
		return 0, 0
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "  ! write %s: %v\n", rel, err)
		return 0, 0
	}
	fmt.Printf("  + wrote: %s\n", rel)
	return 1, 0
}

// writeScaffold generates the lane scaffolding into outRoot (an AOSP tree root). v2 scope:
// the goldfish emu products + their registration in the goldfish AndroidProducts.mk. It NEVER
// overwrites a generated product (hand-tuned once created — reports a skip, Rule 7); the
// AndroidProducts.mk registration is idempotent + snapshotted (.sld-bak). It names the
// next-increment gaps (device/cuttlefish products, bp mirror, finder/aar patch) so the output
// never reads as "fully scaffolded" when it is not (no silent caps).
func writeScaffold(c LaneConfig, outRoot string) int {
	if strings.TrimSpace(outRoot) == "" {
		fmt.Fprintln(os.Stderr, "create: -out <aosp-root> is required to generate files")
		return 2
	}
	info, err := os.Stat(outRoot)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "create: -out %q is not a directory\n", outRoot)
		return 2
	}
	fmt.Printf("Scaffolding lane %q into %s\n\n", c.Name, outRoot)

	wrote, skipped := 0, 0
	if c.Goldfish {
		for _, f := range emuForms {
			if c.FromLane != "" {
				if r, ct, ok := genEmuProductFromLane(c, f, outRoot, c.FromLane); ok {
					w, sk := writeIfAbsent(outRoot, r, ct)
					wrote += w
					skipped += sk
					continue
				}
			}
			rel, content, err := genEmuProduct(c, f)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  ! render %s: %v\n", f.Key, err)
				return 1
			}
			abs := filepath.Join(outRoot, rel)
			if _, err := os.Stat(abs); err == nil {
				fmt.Printf("  = skip (exists): %s\n", rel)
				skipped++
				continue
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				fmt.Fprintf(os.Stderr, "  ! mkdir %s: %v\n", filepath.Dir(rel), err)
				return 1
			}
			if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "  ! write %s: %v\n", rel, err)
				return 1
			}
			fmt.Printf("  + wrote: %s\n", rel)
			wrote++
		}
		// Register the generated products by inserting them into the existing PRODUCT_MAKEFILES
		// list in the goldfish AndroidProducts.mk — the exact form the booted-green holo emu
		// products use (a `:= \` continuation list, no COMMON_LUNCH_CHOICES). Idempotent + snapshotted.
		switch st, err := registerGoldfishProducts(c, outRoot); {
		case err != nil:
			fmt.Fprintf(os.Stderr, "  ! register: %v\n", err)
		case st == patchApplied:
			fmt.Printf("  + registered both products in device/generic/goldfish/AndroidProducts.mk (snapshot .sld-bak)\n")
		case st == patchAlreadyDone:
			fmt.Printf("  = already registered in device/generic/goldfish/AndroidProducts.mk (idempotent)\n")
		default: // patchFileMissing / patchUnrecognized
			fmt.Printf("  ! could not auto-register (AndroidProducts.mk absent/unrecognized under -out); add manually:\n")
			printGoldfishRegGuidance(c)
		}
	}

	if c.Cuttlefish {
		for _, f := range cfForms {
			rel, content, err := genCuttlefishProduct(c, f)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  ! render cf %s: %v\n", f.Key, err)
				return 1
			}
			abs := filepath.Join(outRoot, rel)
			if _, err := os.Stat(abs); err == nil {
				fmt.Printf("  = skip (exists): %s\n", rel)
				skipped++
				continue
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				fmt.Fprintf(os.Stderr, "  ! mkdir %s: %v\n", filepath.Dir(rel), err)
				return 1
			}
			if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "  ! write %s: %v\n", rel, err)
				return 1
			}
			fmt.Printf("  + wrote: %s\n", rel)
			wrote++
		}
		switch st, err := registerCuttlefishProducts(c, outRoot); {
		case err != nil:
			fmt.Fprintf(os.Stderr, "  ! register cf: %v\n", err)
		case st == patchApplied:
			fmt.Printf("  + registered both products in device/google/cuttlefish/AndroidProducts.mk (snapshot .sld-bak)\n")
		case st == patchAlreadyDone:
			fmt.Printf("  = already registered in device/google/cuttlefish/AndroidProducts.mk (idempotent)\n")
		default:
			fmt.Printf("  ! could not auto-register cuttlefish (device/google/cuttlefish/AndroidProducts.mk absent/unrecognized under -out)\n")
		}
	}

	if len(c.Devices) > 0 {
		w, s, fatal := writeDeviceProducts(c, outRoot)
		wrote += w
		skipped += s
		if fatal {
			return 1
		}
	}

	// bp-mirror: clone selected stock subtrees into the lane tree (keep-name) + root namespace.
	if len(c.Forks) > 0 {
		if def := defaultInfraExcludes(c); len(def) > 0 {
			c.ForkExclude = mergeDedup(c.ForkExclude, def)
			fmt.Printf("  (auto-excluded infra for the frameworks/base fork — left stock: %s)\n", strings.Join(def, ", "))
		}
		// -no-compose: leave the AndroidX/Compose/Kotlin-vendored subtrees stock (computed from the real
		// tree, not a stale literal) so the lane never forks Compose it won't build.
		if c.NoCompose {
			if ve := computeVendoredExcludes(outRoot); len(ve) > 0 {
				c.ForkExclude = mergeDedup(c.ForkExclude, ve)
				fmt.Printf("  (-no-compose: left %d Compose/AndroidX/Kotlin subtrees stock)\n", len(ve))
			}
		}
		copied, nsWrote, fatal := runMirror(c, outRoot, c.Forks)
		wrote += copied + nsWrote
		if fatal {
			return 1
		}
		// requalify: repoint the cloned bp's internal //<root>/… labels to the lane path where
		// the target was forked (fork-boundary aware) — so large forks (frameworks/base) resolve.
		runRequalify(c, outRoot)
		// Lane-SOURCED fork: the clone is verbatim, so it still points at the SOURCE lane —
		// //frameworks-<src>/… labels, bare include_dirs paths, and paths embedded in genrule cmd
		// strings. The new lane's finder drops the source lane, so a qualified label dangles; a
		// BARE path is worse, resolving against the source lane's files (still on disk) so the
		// clone compiles green against the wrong tree.
		if c.FromLane != "" {
			runRequalifyFromLane(c, outRoot, c.FromLane)
			// Blueprint files are not the only place a clone carries its source lane's paths:
			// .proto imports, C/C++ #includes and .mk paths do too, and those failures surface
			// inside GENERATED files (a .pb.h whose #include still names the source lane).
			runRelocateLaneSourcePaths(c, outRoot, c.FromLane)
		}
		// rename model only (no-op for keep-name): tier 1 installables (+overrides), then tier 2
		// libraries (rename + repoint every dep ref in lockstep).
		runRenameInstallables(c, outRoot)
		runRenameLibs(c, outRoot)
		runRenameFrameworkClass(c, outRoot)       // no-op: framework-class stays KEEP-NAME (Model-A hybrid)
		runNoComposeDropDeps(c, outRoot)          // no-op unless -no-compose: drop dangling Compose dep refs
		runNoComposeScopeSystemUISrcs(c, outRoot) // no-op unless -no-compose: scope SystemUI srcs to kotlin/**
	}

	// Route manifest — new file, lean drop-list seed (populate from build evidence later).
	if mf, err := emitRouteManifestFrom(c.Name, c.CamelCase, outRoot, c.FromLane); err == nil {
		w, s := writeIfAbsent(outRoot, filepath.Join("."+c.Name, c.Name+"_bp_route_manifest.json"), string(mf))
		wrote += w
		skipped += s
	} else {
		fmt.Fprintf(os.Stderr, "  ! route manifest: %v\n", err)
	}

	// In-place soong patches: STAGED for review (preview-then-apply, T-directed) — never
	// touches the live build/soong/*.go here.
	stagedN, fatal := stageSoongPatches(c, outRoot)
	if fatal {
		return 1
	}

	fmt.Printf("\n%d file(s) written, %d skipped, %d soong patch(es) staged.\n", wrote, skipped, stagedN)
	if stagedN > 0 {
		fmt.Printf("Review the diffs above, then commit: sovereign-lane-surgeon apply -out %s\n", outRoot)
	}
	if len(c.Forks) == 0 {
		fmt.Printf("\nTIP: pass -fork frameworks/<subtree>,packages/<subtree> to clone stock subtrees into\n     frameworks-%s/ + packages-%s/ (keep-name — verbatim, no ref rewrite).\n", c.Name, c.Name)
	}
	if c.KeepName {
		forkedFwkBase := false
		for _, f := range c.Forks {
			if strings.HasPrefix(strings.TrimLeft(f, "/"), "frameworks/base") {
				forkedFwkBase = true
			}
		}
		if !forkedFwkBase {
			fmt.Printf("\n⚠ KEEP-NAME: this lane's lunch enables the aar.go framework-class suppressors, which hide\n")
			fmt.Printf("  stock framework-res/framework/services EXPECTING a lane replacement. You MUST fork the\n")
			fmt.Printf("  framework-class (-fork frameworks/base) or a full build fails with 'framework-minus-apex\n")
			fmt.Printf("  requires non-existent module'. (`m nothing` analyzes fine without it — verified.)\n")
		}
	}
	fmt.Printf("\nNEXT (opt-in, after the keep-name lane builds green): prefix installables/libs\n     (Holo<Name>+overrides / %s-<name>) as a separate pass.\n", c.LibPrefix)
	fmt.Printf("VERIFY: aosp_build_capture -lane %s -build-cmd 'm -jN nothing' then `sovereign-lane-surgeon audit`.\n", c.Name)
	return 0
}

// patchStatus is the outcome of an in-place make-file registration patch.
type patchStatus int

const (
	patchApplied      patchStatus = iota // inserted the product lines
	patchAlreadyDone                     // lane products already registered (idempotent)
	patchFileMissing                     // the target AndroidProducts.mk is not under -out
	patchUnrecognized                    // exists but has no PRODUCT_MAKEFILES continuation list
)

// registerGoldfishProducts inserts the generated emu products into the PRODUCT_MAKEFILES list
// in <outRoot>/device/generic/goldfish/AndroidProducts.mk (the exact form the booted-green
// holo emu products use). Idempotent (skips if already present); snapshots the file to
// .sld-bak before the in-place edit (Rule 7 — the tree is not git, so a copy is the only undo).
func registerGoldfishProducts(c LaneConfig, outRoot string) (patchStatus, error) {
	abs := filepath.Join(outRoot, "device", "generic", "goldfish", "AndroidProducts.mk")
	raw, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return patchFileMissing, nil
		}
		return patchFileMissing, err
	}
	phoneMk := fmt.Sprintf("$(LOCAL_DIR)/64bitonly/product/sdk_phone64_%s.mk", c.Name)
	tabletMk := fmt.Sprintf("$(LOCAL_DIR)/64bitonly/product/sdk_tablet_%s.mk", c.Name)
	if strings.Contains(string(raw), phoneMk) {
		return patchAlreadyDone, nil
	}
	lines := strings.Split(string(raw), "\n")
	insertAt := -1
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "PRODUCT_MAKEFILES") && strings.HasSuffix(t, "\\") {
			insertAt = i
			break
		}
	}
	if insertAt < 0 {
		return patchUnrecognized, nil
	}
	out := make([]string, 0, len(lines)+2)
	out = append(out, lines[:insertAt+1]...)
	out = append(out, "    "+phoneMk+" \\", "    "+tabletMk+" \\")
	out = append(out, lines[insertAt+1:]...)

	bak := abs + ".sld-bak"
	if _, serr := os.Stat(bak); os.IsNotExist(serr) {
		if werr := os.WriteFile(bak, raw, 0o644); werr != nil {
			return patchUnrecognized, werr
		}
	}
	if werr := os.WriteFile(abs, []byte(strings.Join(out, "\n")), 0o644); werr != nil {
		return patchUnrecognized, werr
	}
	return patchApplied, nil
}

// printGoldfishRegGuidance prints the manual registration lines when auto-patch cannot apply.
func printGoldfishRegGuidance(c LaneConfig) {
	fmt.Printf("      Add to the PRODUCT_MAKEFILES list in device/generic/goldfish/AndroidProducts.mk:\n")
	for _, f := range emuForms {
		fmt.Printf("        $(LOCAL_DIR)/64bitonly/product/%s_%s.mk \\\n", f.ProdStem, c.Name)
	}
}

// ─── Cuttlefish products (§23.1 step 5) — the device-faithful virtual target ───

// cfForm captures the phone/tablet deltas for a cuttlefish product. Both live under
// vsoc_x86_64/phone/; the tablet only adds PRODUCT_CHARACTERISTICS and is selected at
// launch via `launch_cvd --config=tablet`.
type cfForm struct {
	Key        string // "phone" / "tablet" — in PRODUCT_NAME + PRODUCT_MODEL
	Title      string // "Phone" / "Tablet"
	FilePrefix string // "" → aosp_cf_<lane>.mk ; "tablet_" → aosp_cf_tablet_<lane>.mk
	Tablet     bool
}

var cfForms = []cfForm{
	{Key: "phone", Title: "Phone", FilePrefix: ""},
	{Key: "tablet", Title: "Tablet", FilePrefix: "tablet_", Tablet: true},
}

type cfTmplData struct {
	Name        string
	CamelCase   string
	Form        cfForm
	ProductName string // aosp_cf_x86_64_<form>_<lane>
}

// cuttlefishProductTmpl mirrors aosp_cf_<lane>.mk. Universal structure is filled in
// (inherit the stock cf base + the lane cf common; PRODUCT_NAME/DEVICE/…); the
// lane-specific artifact-path allowlist is a TODO slot (each lane's app/lib set differs).
var cuttlefishProductTmpl = template.Must(template.New("cf").Parse(`#
# Copyright (C) 2026 The Android Open Source Project
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

# {{.CamelCase}} cuttlefish {{.Form.Title}} — the device-faithful virtual target (virtio
# HALs; what AOSP CI runs). Inherits the stock x86_64 phone cuttlefish base, then layers
# the {{.Name}} lane's framework/package routing via shared/{{.Name}}/common.mk.
{{if .Form.Tablet}}# Large tablet DISPLAY (sw720dp) is chosen at launch: launch_cvd --config=tablet.
{{end}}$(call inherit-product, device/google/cuttlefish/vsoc_x86_64/phone/aosp_cf.mk)
$(call inherit-product, device/google/cuttlefish/shared/{{.Name}}/common.mk)

# TODO({{.Name}}): allowlist any lane-specific artifacts the GSI artifact-path check flags
# (wallpaper .so/.apk, re-signed apps, …). Example (the Holo lane's wallpapers):
#   PRODUCT_ARTIFACT_PATH_REQUIREMENT_ALLOWED_LIST += \
#       system/app/{{.CamelCase}}SpiralWallpaper/{{.CamelCase}}SpiralWallpaper.apk \
#       system/lib64/lib{{.Name}}spiral_vk.so
{{if .Form.Tablet}}
PRODUCT_CHARACTERISTICS := tablet
{{end}}
PRODUCT_NAME := {{.ProductName}}
PRODUCT_DEVICE := vsoc_x86_64
PRODUCT_BRAND := Android
PRODUCT_MANUFACTURER := Google
PRODUCT_MODEL := AOSP {{.CamelCase}} on Cuttlefish x86_64 {{.Form.Key}}
`))

// genCuttlefishProduct renders one cuttlefish product file: repo-relative path + content.
func genCuttlefishProduct(c LaneConfig, f cfForm) (string, string, error) {
	data := cfTmplData{
		Name: c.Name, CamelCase: c.CamelCase, Form: f,
		ProductName: fmt.Sprintf("aosp_cf_x86_64_%s_%s", f.Key, c.Name),
	}
	var sb strings.Builder
	if err := cuttlefishProductTmpl.Execute(&sb, data); err != nil {
		return "", "", err
	}
	fname := fmt.Sprintf("aosp_cf_%s%s.mk", f.FilePrefix, c.Name)
	rel := filepath.Join("device", "google", "cuttlefish", "vsoc_x86_64", "phone", fname)
	return rel, sb.String(), nil
}

// registerCuttlefishProducts patches device/google/cuttlefish/AndroidProducts.mk, inserting
// the products into BOTH the PRODUCT_MAKEFILES list (name:path form) and COMMON_LUNCH_CHOICES.
// Idempotent (skips if present) + snapshotted (.sld-bak). Tab-indented to match the file.
func registerCuttlefishProducts(c LaneConfig, outRoot string) (patchStatus, error) {
	abs := filepath.Join(outRoot, "device", "google", "cuttlefish", "AndroidProducts.mk")
	raw, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return patchFileMissing, nil
		}
		return patchFileMissing, err
	}
	phoneProd := fmt.Sprintf("aosp_cf_x86_64_phone_%s", c.Name)
	if strings.Contains(string(raw), phoneProd+":") {
		return patchAlreadyDone, nil
	}
	var mkEntries, lunchEntries []string
	for _, f := range cfForms {
		prod := fmt.Sprintf("aosp_cf_x86_64_%s_%s", f.Key, c.Name)
		fname := fmt.Sprintf("aosp_cf_%s%s.mk", f.FilePrefix, c.Name)
		mkEntries = append(mkEntries, fmt.Sprintf("\t%s:$(LOCAL_DIR)/vsoc_x86_64/phone/%s \\", prod, fname))
		lunchEntries = append(lunchEntries, fmt.Sprintf("\t%s-trunk_staging-userdebug \\", prod))
	}
	out := insertAfterListHead(strings.Split(string(raw), "\n"), "PRODUCT_MAKEFILES", mkEntries)
	out = insertAfterListHead(out, "COMMON_LUNCH_CHOICES", lunchEntries)
	if out == nil {
		return patchUnrecognized, nil
	}
	bak := abs + ".sld-bak"
	if _, serr := os.Stat(bak); os.IsNotExist(serr) {
		if werr := os.WriteFile(bak, raw, 0o644); werr != nil {
			return patchUnrecognized, werr
		}
	}
	if werr := os.WriteFile(abs, []byte(strings.Join(out, "\n")), 0o644); werr != nil {
		return patchUnrecognized, werr
	}
	return patchApplied, nil
}

// insertAfterListHead inserts entries immediately after the `<varName> ... \` continuation
// head line. Returns nil (unrecognized) if no such head is found — callers must nil-check,
// so a partial two-list patch never half-writes.
func insertAfterListHead(lines []string, varName string, entries []string) []string {
	if lines == nil {
		return nil
	}
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, varName) && strings.HasSuffix(t, "\\") {
			out := make([]string, 0, len(lines)+len(entries))
			out = append(out, lines[:i+1]...)
			out = append(out, entries...)
			out = append(out, lines[i+1:]...)
			return out
		}
	}
	return nil
}
