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

import "regexp"

// BlockerClass is one entry of the §22 sovereign-lane blocker taxonomy.
//
//	Name      short slug.
//	Layer     which build layer it fires in: soong | compile | make | governance | infra.
//	Auto      true if the recipe is mechanically applicable (a doctor -apply candidate);
//	          false if it needs a human/UX judgment call.
//	LaneDefect false for infra/environmental classes (transient crashes) — the tool must
//	          NEVER report these as a lane blocker.
//	Sig       regexp matched against a failing module's error text.
//	Recipe    the fix, one line.
type BlockerClass struct {
	Name       string
	Layer      string
	Auto       bool
	LaneDefect bool
	Sig        *regexp.Regexp
	Recipe     string
}

func re(s string) *regexp.Regexp { return regexp.MustCompile(s) }

// Classes are ordered most-specific-first; classification takes the first match.
// Signatures + recipes mirror holo_lane_sovereignty_handoff_20260711.md §22.2.
var Classes = []BlockerClass{
	// ── E. Infra/environmental — NOT lane defects (checked FIRST so a flaky crash is never mislabeled) ──
	{
		Name: "transient-jvm-crash", Layer: "infra", Auto: false, LaneDefect: false,
		Sig:    re(`A fatal error has been detected by the Java Runtime|SIGSEGV.*libjvm|hs_err_pid|Problematic frame`),
		Recipe: "Environmental (JVM/GC crash under -jN memory pressure), NOT a lane defect. Retry at a lower -jN; a clean retry proves it transient. Often lands on a shared/external module.",
	},
	{
		Name: "java-version-parse-tool-lag", Layer: "infra", Auto: false, LaneDefect: false,
		Sig:    re(`Java parsing error:.*Found\s+"yield"|Code processing error`),
		Recipe: "Usually NON-FATAL: a source-parsing codegen tool (e.g. protolog srcgen) can't parse a modern Java feature (yield) in a forked source; it warns+skips. Not a build blocker — update the tool's parser or accept the skip.",
	},
	// ── D. Packaging/governance ──
	{
		Name: "apex-allowed-deps-gate", Layer: "governance", Auto: true, LaneDefect: true,
		Sig:    re(`new-allowed-deps\.txt\.check|allowed_deps\.txt|apex-allowed-deps-error`),
		Recipe: "Stock mainline-release governance gate; a fork's re-themed APEX apps legitimately change the dep-set. Set UNSAFE_DISABLE_APEX_ALLOWED_DEPS_CHECK=true in the lane build env (main.mk gates the check on it). Env-only; keeps stock pure.",
	},
	{
		// Signature INFERRED from source analysis (build/soong/android/namespace.go's
		// namespaceProperties has only Imports []string) - not yet confirmed against a real build's
		// exact wording; broadened to tolerate a few plausible phrasings rather than one guess.
		Name: "invalid-namespace-name-property", Layer: "soong", Auto: true, LaneDefect: true,
		Sig:    re(`unrecognized property "name"|unknown.*property.*"name"|soong_namespace.*name.*not (a|recognized)`),
		Recipe: "soong_namespace only supports imports: [] - its own name is derived from the directory, never a settable property. Delete the name: line (mirror a working soong_namespace in the same tree for the correct form).",
	},
	{
		// Wording INFERRED from the BUILD_BROKEN_SRC_DIR_IS_WRITABLE/RW_ALLOWLIST mutual-exclusion
		// check's likely phrasing, not yet confirmed against a raw build panic verbatim.
		Name: "env-var-mutex-conflict", Layer: "governance", Auto: true, LaneDefect: true,
		Sig:    re(`BUILD_BROKEN_SRC_DIR_IS_WRITABLE.*BUILD_BROKEN_SRC_DIR_RW_ALLOWLIST|cannot set both.*BUILD_BROKEN_SRC_DIR`),
		Recipe: "BUILD_BROKEN_SRC_DIR_IS_WRITABLE and BUILD_BROKEN_SRC_DIR_RW_ALLOWLIST are mutually exclusive. Unset one (env-only) - global IS_WRITABLE is simplest; the ALLOWLIST is for scoping to specific paths.",
	},
	// ── B. Compile/link ──
	{
		Name: "mechanical-rewrite-path-overreach", Layer: "compile", Auto: true, LaneDefect: true,
		Sig:    re(`fatal error: '(frameworks|packages|system|hardware|device)-[a-z0-9]+/[^']*' file not found`),
		Recipe: "A frameworks/→frameworks-<lane>/ (etc.) rewrite hit an #include/ref for a subtree the lane did NOT fork. Revert the ref to the stock path (shared plumbing — restore-to-stock).",
	},
	{
		Name: "half-reroute-type-mismatch", Layer: "compile", Auto: false, LaneDefect: true,
		Sig:    re(`incompatible types:.*cannot be converted to`),
		Recipe: "A consumer's import was rerouted to a new lib while its base still returns the old type. Align the seam — revert the consumer to match its base (low blast) or migrate the base (high blast, deferred).",
	},
	{
		Name: "apk-resource-gap", Layer: "compile", Auto: true, LaneDefect: true,
		Sig:    re(`error: resource [a-zA-Z]+/[^ ]+ not found|resource .* not found`),
		Recipe: "A res value stock resolves from an aar the fork doesn't provide. Define the resource locally (plumbing) + flag the UX re-authoring.",
	},
	{
		Name: "kapt-under-modern-kotlinc", Layer: "compile", Auto: false, LaneDefect: true,
		Sig:    re(`is not an annotation|kapt.*could not resolve|could not resolve ColorScheme`),
		Recipe: "kapt (K1) stub-gen fails under the lane's 2.1.x kotlinc referencing dropped types. Disable-if-redundant (enabled:false) / migrate the processor to KSP / pin the module to stock 1.9.x kotlinc.",
	},
	{
		// split-package vs shim-api-gap both surface as cannot-find-symbol; flag for triage.
		Name: "cannot-find-symbol", Layer: "compile", Auto: false, LaneDefect: true,
		Sig:    re(`cannot find symbol|unresolved reference`),
		Recipe: "TRIAGE: split-package collision (two lane libs define the same pkg.Class; a consumer pulls both — superset the winning lib or dedupe) OR shim API-gap (a re-authored shim lacks a called method — add an additive stub). Check whether the symbol exists in a sibling lib.",
	},
	// ── A. Soong analysis ──
	{
		// Observed android-17.0.0_r1, 2026-09-02: build/soong/cc/config/global.go IllegalFlags.
		Name: "illegal-cflag", Layer: "soong", Auto: true, LaneDefect: true,
		Sig:    re("cflags: Illegal flag `"),
		Recipe: "The target's Soong rejects a flag the mirrored tree still passes (-pedantic in AOSP 15 device trees). `create -stock` drops the target's IllegalFlags list from every mirrored Blueprint; by hand: cmd/bpdropcflag -flags <flag> <Android.bp>.",
	},
	{
		// Observed android-17.0.0_r1: libpixelhealth pinned health-V4 while its translate lib linked V5.
		Name: "aidl-version-conflict", Layer: "soong", Auto: true, LaneDefect: true,
		Sig:    re(`depends on multiple versions of the same aidl_interface`),
		Recipe: "A mirrored module pins <iface>-V<n> and links a sibling library from the interface's own Android.bp that pins V<m>. `create -stock` re-pins such modules to the sibling's version (targetcompat.go); by hand: rename-module -subtree <dir> -map <iface>-V<n>-ndk=<iface>-V<m>-ndk.",
	},
	{
		// Observed android-17.0.0_r1: build/soong/fsgen/artifact_path_requirements.go, regardless of `relaxed`.
		Name: "system-props-artifact-path", Layer: "soong", Auto: true, LaneDefect: true,
		Sig:    re(`PRODUCT_SYSTEM_PROPERTIES or PRODUCT_SYSTEM_DEFAULT_PROPERTIES that add properties to the 'system' partition`),
		Recipe: "A device makefile adds system-partition properties, which generic_system.mk's artifact path requirement forbids on this release. Move them to PRODUCT_PRODUCT_PROPERTIES (the message's own suggestion); `create -stock` does this for every mirrored product makefile.",
	},
	{
		// Observed android-17.0.0_r1 full build (cheetah, 144400Z): secilc's final check.
		Name: "sepolicy-neverallow-violation", Layer: "soong", Auto: true, LaneDefect: true,
		Sig:    re(`neverallow check failed`),
		Recipe: "The log names the vendor statement (file:line, after 'allow at … from') and the platform neverallow it violates. Record both in assets/sepolicy_neverallow/MANIFEST so `create -stock` drops the statement while the platform carries the rule; by hand, delete that line.",
	},
	{
		Name: "aidl-dual-supply", Layer: "soong", Auto: true, LaneDefect: true,
		Sig:    re(`Duplicate files found|duplicate.*\.aidl|Duplicate.*aidl`),
		Recipe: "A fork-completed/mainline module's aidl.include_dirs names BOTH frameworks/base/… and frameworks-<lane>/base/…. Lane-scope every aidl.include_dirs in the fork.",
	},
	{
		Name: "bareify-casualty", Layer: "soong", Auto: true, LaneDefect: true,
		Sig:    re(`module source path .* does not exist|depends on itself|source path .* does not exist`),
		Recipe: "A ref-rewrite over-stripped a qualified //path:module → bare module (breaks srcs as a nonexistent file). AST-requalify srcs-family; strip dep/license props.",
	},
	{
		Name: "partial-fork-gap", Layer: "soong", Auto: true, LaneDefect: true,
		Sig:    re(`Exactly one aconfig_declarations|missing dependencies:.*(flags|aconfig)|rule_builder paths cannot be nil`),
		Recipe: "A forked subtree dropped a sibling its consumers need (aconfig_declarations / *_aconfig_library wrapper / api filegroup / license). Diff the fork's bp vs its stock parallel; copy the missing block + local srcs.",
	},
	{
		Name: "namespace-visibility-leak", Layer: "soong", Auto: true, LaneDefect: true,
		Sig:    re(`is not visible to this module|depends on .* which is not visible|violates visibility`),
		Recipe: "Stock bp carrying //frameworks-<lane>/… grants, or a lane namespace-decl stranding consumers. Use soong laneCanonicalPkgs (lane path ≡ its stock parallel for visibility) + restore stock bp to pristine.",
	},
	{
		Name: "duplicate-module-cross-device", Layer: "soong", Auto: true, LaneDefect: true,
		Sig:    re(`module ".*" already defined|already defined in [^ ]+\.bp`),
		Recipe: "Two device/hardware trees define the same module name (real precedent: gs201's and zuma's libacryl_hdr_plugin collided). rename-module -map \"old=new\" one copy + its srcs, mirroring the zuma libacryl_hdr_plugin_zuma fix.",
	},
	// ── C. Product/make ──
	{
		// Observed android-17.0.0_r1: tangorpro's uwb_calibration_country.mk re-runs its generator
		// via $(shell) and the mirrored script had lost its executable bit (embed.FS keeps no modes).
		Name: "generated-mk-not-up-to-date", Layer: "make", Auto: true, LaneDefect: true,
		Sig:    re(`is not up to date, please run`),
		Recipe: "A self-checking generated makefile could not re-run its generator — usually the script is not executable (mirrored without its mode bit). chmod +x the named script; the surgeon's exec manifest (assets/aosp15_device.exec) restores modes on materialize.",
	},
	{
		// Observed android-17.0.0_r1: fips140.ko listed in vendor_kernel_boot.modules.load AND injected
		// by the board (BOARD_PREBUILT_VENDOR_KERNEL_RAMDISK_KERNEL_MODULES).
		Name: "kernel-module-rule-collision", Layer: "make", Auto: true, LaneDefect: true,
		Sig:    re("overriding commands for target `[^']*\\.ko'"),
		Recipe: "A kernel module is named both by the prebuilt dir's modules.load and by the SoC board's own ramdisk-module list, so kati defines its depmod rule twice. Drop it from the list (assemble-kernel omits board-injected modules automatically).",
	},
	{
		Name: "board-config-multiple-definitions", Layer: "make", Auto: false, LaneDefect: true,
		Sig:    re(`Multiple board config files for TARGET_DEVICE`),
		Recipe: "A mirrored/copied device dir kept a BoardConfig.mk that collides with an existing one for the same TARGET_DEVICE (board_config.mk globs by TARGET_DEVICE, not by product path). Exclude BOARD subdirs from the copy unless this is that device's ONLY copy (a lane fork always excludes them; a stock revival keeps them only the first time that exact device is seeded).",
	},
	{
		Name: "literal-name-graph", Layer: "make", Auto: false, LaneDefect: true,
		Sig:    re(`No rule to make target|target pattern contains no|missing required`),
		Recipe: "kati resolves a module by literal name (required:/PRODUCT_BOOT_JARS/misc_prebuilt). Keep-name, or add a phony alias.",
	},
}

// KNOWN GAP (2026-09, physical-device-revival session): "fabricated/incomplete stub content" was
// the single biggest real bug class hit reviving Panther/Cheetah (6 Android.bp files replaced with
// plausible-looking invented modules referencing each other but nothing real) - and it does NOT
// fit this taxonomy's regex-on-error-text model well. It often produces NO error at all where it's
// introduced (a stub just parses fine and silently omits real modules), surfacing several steps
// removed instead (an unrelated "cannot find libExynosHWCService" elsewhere). Classify() can't
// catch this class as written; it needs a content-parity check against a reference source tree
// (diff the mirrored/copied .bp against its real stock counterpart), not error-text matching. Left
// as a flagged gap rather than a forced-fit class - a real design conversation, not a one-line fix.

// classForText returns the first matching BlockerClass, or nil if unclassified.
func classForText(text string) *BlockerClass {
	for i := range Classes {
		if Classes[i].Sig.MatchString(text) {
			return &Classes[i]
		}
	}
	return nil
}
