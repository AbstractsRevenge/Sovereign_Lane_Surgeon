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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	parser "github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/internal/blueprint/parser"
)

// renamepass.go — the RENAME-model installable transform (§5/§6, T-directed "Testing1SystemUI").
// For each installable app module in a cloned lane bp it rewrites `name:"X"` → `name:"<Camel>X"`
// and injects `overrides:["X"]` (so the lane app suppresses the stock one). AST-safe via the
// vendored Blueprint parser. This is the low-risk TIER 1 (leaf apps). Libraries (tier 2,
// <lane>-<name>) and the framework class (tier 3, stem/phony — the services.jar bootloop tier)
// are separate, higher-care passes. A multi-module app like SystemUI needs its whole interior
// (core/pods/libs + refs) renamed in lockstep — the tier even the NexusM reference deferred.

// installableAppTypes are the Soong module types that install an APK (candidates for <Lane><Name>).
var installableAppTypes = map[string]bool{
	"android_app":        true,
	"android_app_import": true,
}

// renameInstallableBp renames installable app modules in one bp to the lane prefix + overrides.
// Returns whether anything changed. Idempotent (skips already-prefixed names).
func renameInstallableBp(content []byte, camel string) ([]byte, bool, error) {
	file, errs := parser.Parse("", bytes.NewReader(content))
	if len(errs) > 0 {
		return nil, false, errs[0]
	}
	changed := false
	for _, def := range file.Defs {
		m, ok := def.(*parser.Module)
		if !ok || !installableAppTypes[m.Type] {
			continue
		}
		var nameProp, overridesProp *parser.Property
		for _, p := range m.Properties {
			switch p.Name {
			case "name":
				nameProp = p
			case "overrides":
				overridesProp = p
			}
		}
		if nameProp == nil {
			continue
		}
		ns, ok := nameProp.Value.(*parser.String)
		if !ok {
			continue
		}
		stock := ns.Value
		if strings.HasPrefix(stock, camel) {
			continue // already renamed — idempotent
		}
		if _, isFwc := frameworkClassStem[stock]; isFwc {
			continue // framework class (e.g. framework-res is an android_app) → tier 3, not tier 1
		}
		ns.Value = camel + stock
		// Inject overrides:["<stock>"] so the renamed lane app suppresses the stock variant.
		if overridesProp == nil {
			m.Properties = append(m.Properties, &parser.Property{
				Name:  "overrides",
				Value: &parser.List{Values: []parser.Expression{&parser.String{Value: stock}}},
			})
		} else if lst, ok := overridesProp.Value.(*parser.List); ok {
			has := false
			for _, e := range lst.Values {
				if s, ok := e.(*parser.String); ok && s.Value == stock {
					has = true
				}
			}
			if !has {
				lst.Values = append(lst.Values, &parser.String{Value: stock})
			}
		}
		changed = true
	}
	if !changed {
		return content, false, nil
	}
	out, err := parser.Print(file)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// --- Tier 3: framework-class rename (stem + phony) — the services.jar bootloop tier ---
//
// ⚠ HIGH CARE. Grounded from the NexusM reference: a framework-class module X is renamed to
// X-<lane>, given `stem:"<install-name>"` so its installed artifact keeps the canonical name
// (framework-res.apk / framework.jar / services.jar), and a `phony{name:"X", required:["X-<lane>"]}`
// bridges every literal-name consumer (PRODUCT_BOOT_JARS/SYSTEM_SERVER_JARS, make refs). framework-res
// also needs its X-<lane> added to aar.go isFrameworkResClassName. Renaming these WRONG is exactly
// what caused the historical services.jar boot-loop — this tier MUST be device-BOOT-verified (not
// just build-green), one module at a time, before it is trusted (md hard rule).

// frameworkClassStem maps a framework-class module to its install-artifact stem (NOT uniform).
var frameworkClassStem = map[string]string{
	"framework-res":        "framework-res",
	"framework-minus-apex": "framework",
	"services":             "services",
}

// phonyModule builds a `phony { name: orig, required: [renamed] }` bridge module.
func phonyModule(orig, renamed string) *parser.Module {
	return &parser.Module{
		Type: "phony",
		Map: parser.Map{Properties: []*parser.Property{
			{Name: "name", Value: &parser.String{Value: orig}},
			{Name: "required", Value: &parser.List{Values: []parser.Expression{&parser.String{Value: renamed}}}},
		}},
	}
}

// setOrAddString sets prop=val on a module (adds the property if absent).
func setOrAddString(m *parser.Module, prop, val string) {
	for _, p := range m.Properties {
		if p.Name == prop {
			if s, ok := p.Value.(*parser.String); ok {
				s.Value = val
			}
			return
		}
	}
	m.Properties = append(m.Properties, &parser.Property{Name: prop, Value: &parser.String{Value: val}})
}

// removeProp deletes a top-level property from a module. Returns true if it removed one.
func removeProp(m *parser.Module, prop string) bool {
	for i, p := range m.Properties {
		if p.Name == prop {
			m.Properties = append(m.Properties[:i], m.Properties[i+1:]...)
			return true
		}
	}
	return false
}

// setOrAddBool sets (or appends) a boolean property. Returns true if it ADDED the property (was absent).
func setOrAddBool(m *parser.Module, prop string, val bool) bool {
	tok := "false"
	if val {
		tok = "true"
	}
	for _, p := range m.Properties {
		if p.Name == prop {
			if b, ok := p.Value.(*parser.Bool); ok {
				b.Value, b.Token = val, tok
			}
			return false
		}
	}
	m.Properties = append(m.Properties, &parser.Property{Name: prop, Value: &parser.Bool{Value: val, Token: tok}})
	return true
}

// renameFrameworkClassBp renames framework-class modules (X → X-<lane> + stem + phony bridge).
func renameFrameworkClassBp(content []byte, lane string) ([]byte, bool, error) {
	file, errs := parser.Parse("", bytes.NewReader(content))
	if len(errs) > 0 {
		return nil, false, errs[0]
	}
	changed := false
	var phonies []*parser.Module
	for _, def := range file.Defs {
		mod, ok := def.(*parser.Module)
		if !ok {
			continue
		}
		var nameProp *parser.Property
		for _, p := range mod.Properties {
			if p.Name == "name" {
				nameProp = p
			}
		}
		if nameProp == nil {
			continue
		}
		ns, ok := nameProp.Value.(*parser.String)
		if !ok {
			continue
		}
		stem, isFwc := frameworkClassStem[ns.Value]
		if !isFwc {
			continue
		}
		orig := ns.Value
		renamed := orig + "-" + lane
		ns.Value = renamed
		setOrAddString(mod, "stem", stem)
		phonies = append(phonies, phonyModule(orig, renamed))
		changed = true
	}
	if !changed {
		return content, false, nil
	}
	for _, ph := range phonies {
		file.Defs = append(file.Defs, ph)
	}
	out, err := parser.Print(file)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// runRenameFrameworkClass — DELIBERATE NO-OP under the app-naming (Model-A hybrid) model (2026-08-03).
//
// FRAMEWORK-CLASS modules (framework-res, framework-minus-apex, services) stay KEEP-NAME; only
// identity apps (tier 1) and libs (tier 2) are renamed. Renaming framework-class to X-<lane> + stem +
// phony was PROVEN to break the build: a phony "framework-minus-apex" does not implement
// hiddenAPIModule (platform-bootclasspath fails), and a phony "services" is a non-java module
// (car-frameworks-service depends on non-java module "services"). Keep-name works because the finder
// drops the stock parallel (existingStockParallel<Lane>'s tree-prefix branch) so the lane's keep-name
// module claims the canonical slot, and aar.go's isLaneLunch-gated shouldSuppressStock* suppressors
// (enabled by PatchIsLaneLunch for BOTH models) vacate the stock kati slot — identical to the
// keep-name model. Framework-class is therefore handled the same way in both models; the ONLY
// difference the app-naming model keeps is the renamed IDENTITY APPS (tier 1) + libs (tier 2).
//
// The stem+phony machinery (renameFrameworkClassBp / phonyModule / frameworkClassStem) and the aar.go
// appendFrameworkResCase patch are RETAINED (still unit-tested in renamepass_test.go / soongpatch_test.go)
// purely as a record of the rejected approach; they are no longer wired into the scaffold pipeline.
func runRenameFrameworkClass(c LaneConfig, outRoot string) (changed, failed int) {
	fmt.Println("\nframework-class: KEEP-NAME (Model-A hybrid — not renamed; finder-drop + aar.go suppressors claim the canonical slot).")
	return 0, 0
}

// --- Tier 2: library rename + dep-ref repoint ---

// libRenameTypes are the module types renamed in tier 2 (compile-time surfaces — NOT the
// installable apps of tier 1, NOT the framework class of tier 3).
var libRenameTypes = map[string]bool{
	"java_library": true, "java_library_static": true, "java_library_host": true,
	"android_library": true, "java_defaults": true, "java_sdk_library": true,
	"java_import": true, "java_genrule": true, "genrule": true, "filegroup": true,
	"cc_library": true, "cc_library_static": true, "cc_library_shared": true,
	"cc_library_headers": true, "cc_defaults": true, "aidl_interface": true,
}

// frameworkClassNames are kept out of tier 2 — they need tier-3 stem/phony (the bootloop tier).
var frameworkClassNames = map[string]bool{
	"framework-res": true, "framework": true, "framework-minus-apex": true,
	"framework-minus-apex-intdefs": true, "services": true, "services.core": true,
	"services.core.unboosted": true,
}

// depNameProps carry BARE module-name lists (§11-12) — repoint the bare names.
var depNameProps = map[string]bool{
	"static_libs": true, "libs": true, "shared_libs": true, "whole_static_libs": true,
	"header_libs": true, "runtime_libs": true, "defaults": true, "required": true,
	"export_shared_lib_headers": true, "export_static_lib_headers": true, "export_header_lib_headers": true,
	// apex CONTENT props (bare module names): an apex packaging a renamed identity app/lib must repoint
	// them, else it references the stale keep-name (com.android.cellbroadcast apps:["CellBroadcastApp"]
	// after the app renamed to NexusmCellBroadcastApp → "app dependency ... must have updatable: true"
	// against the empty re-export stub).
	"apps": true, "native_shared_libs": true, "java_libs": true, "bootclasspath_fragments": true,
	"systemserverclasspath_fragments": true, "prebuilts": true, "jni_libs": true, "binaries": true,
	"rust_libs": true, "sh_binaries": true,
	// Rust modules (rust_ffi/rust_library/rust_test/rust_defaults) name their deps here — including the
	// aidl -rust bindings (android.hardware.bluetooth.offload.a2dp-rust) whose base interface the lane
	// renamed. Soong's property is `rustlibs` (not rust_libs); rlibs/dylibs/proc_macros are the variants.
	"rustlibs": true, "rlibs": true, "dylibs": true, "proc_macros": true,
	// combined_apis (frameworks/base/api) names the API-surface framework modules by bare name in these
	// classpath lists. When a lane forks api keep-name (fork-shared-infra) its refs to the lane's renamed
	// framework modules (framework-location → <Camel>framework-location) must repoint here, or the merged
	// API dangles. Always module-name lists in Soong, so a non-renamed name is left untouched.
	"bootclasspath": true, "conditional_bootclasspath": true, "system_server_classpath": true,
	// bootclasspath_fragment / systemserverclasspath_fragment name the apex boot & service jars by bare
	// module name here. The lane renames those mainline modules (framework-permission → Camel…, service-wifi
	// → Camel…), so the apex fragments must repoint or the fragment dangles ("… depends on undefined module
	// framework-permission"). Surfaces only once the foundational api chain resolves (masked before).
	"contents": true, "standalone_contents": true,
	// aidl_interface `imports:` names the OTHER aidl_interfaces it depends on, versioned (foo-V1). A lane
	// that renames an interface (netd_event_listener_interface → Camel…) must repoint the versioned import
	// or the importer fails ("Import does not exist: netd_event_listener_interface"). The -V<n> suffix is
	// handled by the repoint closure's version-suffix strip.
	"imports": true,
	// generated_* name the genrule modules whose out: headers/sources a cc/rust module consumes. A lane
	// that renames the genrule (BluetoothGeneratedPacketsL2cap_h → Camel…, casimir_rf_packets_cxx_gen →
	// Camel…) must repoint the consumer's generated_headers list or it dangles ("depends on undefined
	// module …_gen"/…_h"). Bare module-name lists, so a non-renamed generator is left untouched.
	"generated_headers": true, "export_generated_headers": true, "generated_sources": true,
	"device_first_generated_headers": true,
	// data_libs/data_bins name test-data modules (cc/rust test `data_libs: [...]`) by bare name — a lane
	// rename of the data lib (libandroid_runtime_lazy → Camel…) must repoint or the test dangles.
	"data_libs": true, "data_bins": true,
	// impl_only_* are java_sdk_library implementation-only deps (framework-minus-apex-headers); the auto-
	// generated <lib>.impl inherits them. extra_check_modules (nested in lint:{}) names lint-checker modules
	// (ProtoLogLintChecker). A lane rename of any of these must repoint or the .impl / lint dangles.
	"impl_only_libs": true, "impl_only_static_libs": true, "extra_check_modules": true,
	// header_jar_override is a single bare module name (framework-minus-apex's header jar). repointExprRefs
	// handles the single-string value the same as a list leaf.
	"header_jar_override": true,
	// uses_libs names the <uses-library> provider MODULES an app needs; a renamed provider must repoint.
	"uses_libs": true, "optional_uses_libs": true,
	// instrumentation_for is the (single) app-under-test a robolectric/instrumentation test targets; a
	// renamed app must repoint or the test dangles ("XRoboTests depends on undefined X").
	"instrumentation_for": true,
	// associates/coverage_libs are robolectric test coverage links to the module(s) under test; a renamed
	// module must repoint or the test dangles.
	"associates": true, "coverage_libs": true,
	// additional_shared_libraries: aidl_interface cpp/ndk backend extra shared libs (module names).
	"additional_shared_libraries": true,
	// export_header_libs: aidl_interface backend re-exported header libs (distinct from export_header_lib_headers).
	"export_header_libs": true,
	// export_llndk_headers: a cc_library's `llndk: { export_llndk_headers: [...] }` names the header libs
	// (cc_library_headers) exported to its LLNDK (vendor) variant. A lane that renames such a header lib
	// (gl_headers → Holo2gl_headers) must repoint the nested ref or the LLNDK variant dangles ("libEGL
	// depends on undefined module gl_headers"). repointProps descends into the llndk map by name.
	"export_llndk_headers": true,
	// deps: android_filesystem / android_system_image list their INSTALLABLE modules here by bare name
	// (microdroid's deps:["libadbd_auth","libadbd_fs",…]). A lane that forks+renames such an installable
	// (libadbd_auth → Holo2libadbd_auth) must repoint the filesystem's deps or it dangles ("microdroid
	// depends on undefined module libadbd_auth"). Always a module-name list in Soong, so a non-renamed
	// name is left untouched.
	"deps": true,
}

// srcsRefProps carry file paths + `:module` filegroup/genrule refs — repoint only the `:module` form.
// Includes the SINGLE-VALUE ref props (manifest/privapp_allowlist/src) that name a genrule or filegroup
// by `:module`: a rename that misses these leaves the consumer referencing the stale keep-name (e.g.
// android_app{manifest:":generate_shim_manifest", privapp_allowlist:":privapp_allowlist_….xml"},
// prebuilt_etc{src:":genrule"}) after the lane renamed the producer to Nexusm<name> — an undefined-module
// straggler. File-path values (no leading ":") are ignored by the repoint, so listing them here is safe.
var srcsRefProps = map[string]bool{
	"srcs": true, "exclude_srcs": true, "tool_files": true, "java_resources": true, "device_common_srcs": true,
	"manifest": true, "privapp_allowlist": true, "src": true, "additional_manifests": true,
	// key/signing refs are single ":module" values (private_key: ":pvmfw_embedded_key",
	// avb_private_key: ":microdroid_sign_key"); srcsOnly repointing only touches ":"-refs, so a plain
	// "platform" certificate string is left alone while a renamed lane key module is repointed.
	"private_key": true, "avb_private_key": true, "certificate": true,
}

// collectLibRenames walks the lane tree and maps each renameable lib module's name → <Camel>name.
func collectLibRenames(outRoot string, c LaneConfig) map[string]string {
	m := map[string]string{}
	for _, root := range []string{"frameworks-" + c.Name, "packages-" + c.Name} {
		rootDir := filepath.Join(outRoot, root)
		if fi, err := os.Stat(rootDir); err != nil || !fi.IsDir() {
			continue
		}
		filepath.Walk(rootDir, func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || !isBpFile(filepath.Base(p)) {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			file, errs := parser.Parse("", bytes.NewReader(b))
			if len(errs) > 0 {
				return nil
			}
			for _, def := range file.Defs {
				mod, ok := def.(*parser.Module)
				if !ok || !libRenameTypes[mod.Type] {
					continue
				}
				for _, pr := range mod.Properties {
					if pr.Name != "name" {
						continue
					}
					if ns, ok := pr.Value.(*parser.String); ok {
						n := ns.Value
						if !frameworkClassNames[n] && !strings.HasPrefix(n, c.CamelCase) {
							m[n] = c.CamelCase + n
						}
					}
				}
			}
			return nil
		})
	}
	return m
}

// repointExprRefs walks an Expression applying repoint to every string leaf, recursing through the
// forms a dep/srcs property value can take: a plain List, a `[...] + select(...)` concatenation
// (*Operator, Args[0]+Args[1]), and select() branches (*Select, each Case.Value + Append). A plain
// `pr.Value.(*parser.List)` type-assertion misses the last two — which is how a `:module` ref inside
// `srcs: [...] + select(...)` survived a rename (the observed straggler). srcsOnly restricts to
// ":"-prefixed refs (srcs/filegroup form); false repoints bare names too (dep lists). Returns whether
// anything changed.
func repointExprRefs(expr parser.Expression, repoint func(string) string, srcsOnly bool) bool {
	changed := false
	switch v := expr.(type) {
	case *parser.String:
		// srcsOnly restricts to MODULE refs (":module" filegroup form or "//path:module" qualified
		// label), never plain file paths. A "//…:mod" label in srcs is a genrule/filegroup output ref
		// and must repoint like any dep — the old ":"-only filter missed it (native_bridge proxy libc
		// files: srcs: ["//frameworks/libs/…:native_bridge_proxy_libc_files"]).
		if !srcsOnly || strings.HasPrefix(v.Value, ":") || strings.HasPrefix(v.Value, "//") {
			if nv := repoint(v.Value); nv != v.Value {
				v.Value = nv
				changed = true
			}
		}
	case *parser.List:
		for _, e := range v.Values {
			if repointExprRefs(e, repoint, srcsOnly) {
				changed = true
			}
		}
	case *parser.Operator: // "[...] + select(...)" / "list + list"
		if repointExprRefs(v.Args[0], repoint, srcsOnly) {
			changed = true
		}
		if repointExprRefs(v.Args[1], repoint, srcsOnly) {
			changed = true
		}
	case *parser.Select:
		for _, cs := range v.Cases {
			if cs != nil && repointExprRefs(cs.Value, repoint, srcsOnly) {
				changed = true
			}
		}
		if v.Append != nil && repointExprRefs(v.Append, repoint, srcsOnly) {
			changed = true
		}
	}
	return changed
}

// repointCmdLabels repoints ":module" / "//path:module" LABEL tokens embedded in a free-form genrule
// `cmd` string (e.g. "$(location :devicelockcontroller-protos) …"). A genrule's cmd references the same
// modules named in its srcs/tools/tool_files; when those lists are repointed to the renamed name, the
// matching $(location(s) …) labels in cmd MUST follow, or Soong errors "unknown location label :X is not
// in srcs, out, tools or tool_files". A label token starts at ":" or "//" on a boundary (start, or after
// a non-label char such as space or "(") and runs through label chars (incl. ":" for //path:mod and
// "{}" for an output tag). repoint() only rewrites tokens whose base is a renamed lane module, so shell
// text, $(in)/$(out), flags and paths are left untouched.
func repointCmdLabels(s string, repoint func(string) string) (string, bool) {
	isLabelChar := func(c byte) bool {
		return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			c == '_' || c == '.' || c == '-' || c == '+' || c == '@' || c == '/' || c == ':' || c == '{' || c == '}'
	}
	var b strings.Builder
	changed := false
	for i, n := 0, len(s); i < n; {
		start := s[i] == ':' || (s[i] == '/' && i+1 < n && s[i+1] == '/')
		if start && (i == 0 || !isLabelChar(s[i-1])) {
			j := i
			for j < n && isLabelChar(s[j]) {
				j++
			}
			tok := s[i:j]
			nv := repoint(tok)
			b.WriteString(nv)
			if nv != tok {
				changed = true
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String(), changed
}

// repointCmdExpr walks a genrule `cmd` expression (a "str" + "str" + … concatenation, sometimes with a
// select()) and applies repointCmdLabels to every String leaf, so embedded $(location(s) :module) labels
// are repointed in lockstep with srcs/tools.
func repointCmdExpr(expr parser.Expression, repoint func(string) string) bool {
	changed := false
	switch v := expr.(type) {
	case *parser.String:
		if nv, ch := repointCmdLabels(v.Value, repoint); ch {
			v.Value = nv
			changed = true
		}
	case *parser.List: // flags: ["--manifest", "$(location :X)", …]
		for _, e := range v.Values {
			if repointCmdExpr(e, repoint) {
				changed = true
			}
		}
	case *parser.Operator:
		if repointCmdExpr(v.Args[0], repoint) {
			changed = true
		}
		if repointCmdExpr(v.Args[1], repoint) {
			changed = true
		}
	case *parser.Select:
		for _, cs := range v.Cases {
			if cs != nil && repointCmdExpr(cs.Value, repoint) {
				changed = true
			}
		}
		if v.Append != nil && repointCmdExpr(v.Append, repoint) {
			changed = true
		}
	}
	return changed
}

// renameAndRepointBp renames mapped lib modules + repoints every dep-name / srcs `:module` ref to a
// mapped name. lane is used to set `owner:` on renamed aidl_interface modules (Soong requires it
// for an aidl_interface inside a soong_namespace — the rename-model lane keeps its namespace).
// Returns whether it changed.
func renameAndRepointBp(content []byte, rename map[string]string, lane string) ([]byte, bool, error) {
	file, errs := parser.Parse("", bytes.NewReader(content))
	if len(errs) > 0 {
		return nil, false, errs[0]
	}
	changed := false
	repointBase := func(s string) string {
		if nv, ok := rename[s]; ok { // bare dep name
			return nv
		}
		if strings.HasPrefix(s, ":") { // same-package filegroup ref
			if nv, ok := rename[s[1:]]; ok {
				return ":" + nv
			}
		}
		// Fully-qualified label //<root>/<path>:<mod> — a cross-directory ref that names both a path and a
		// module. When the lane renamed <mod>, repoint the name AND requalify the path root to the lane
		// tree (the renamed module now lives at frameworks-<lane>/… / packages-<lane>/…). A label to a
		// module the lane did NOT rename is left untouched (its stock definer stays resolvable). This is
		// the form the native-bridge proxy defaults use (//frameworks/libs/native_bridge_support/…:mod),
		// which neither the bare nor ":"-prefixed cases reach.
		if strings.HasPrefix(s, "//") {
			if ci := strings.LastIndexByte(s, ':'); ci > 2 {
				body, modName := s[2:ci], s[ci+1:]
				if nv, ok := rename[modName]; ok {
					if lanePath, ok2 := laneDirFor(body, lane); ok2 {
						return "//" + lanePath + ":" + nv
					}
					return "//" + body + ":" + nv
				}
			}
		}
		// Derived-suffix-aware: a ref to X-cpp / X.stubs.module_lib / X-aconfig-java is neither X nor :X,
		// so an exact-match repoint leaves it dangling when the base X is renamed (the def now derives
		// <new>-cpp but the consumer still says X-cpp). Strip a known Soong-derived suffix, look up the
		// BASE in the map, and re-append the suffix — using the same derivedSuffixes the undefined-deps
		// census uses (one source of truth), so a base rename carries its aidl/sdk/aconfig backends.
		prefix, bare := "", s
		if strings.HasPrefix(s, ":") {
			prefix, bare = ":", s[1:]
		}
		for _, suf := range derivedSuffixes {
			if strings.HasSuffix(bare, suf) && len(bare) > len(suf) {
				if nv, ok := rename[strings.TrimSuffix(bare, suf)]; ok {
					return prefix + nv + suf
				}
			}
		}
		// Versioned aidl form: <interface>-V<n> (imports:) OR <interface>-V<n>-<backend>
		// (android.frameworks.sensorservice-V1-ndk / -V1-cpp / -V1-java / -V1-rust in dep lists). The
		// -V<n> version tag is not one of the fixed backend suffixes above; consume -V then the digits,
		// and accept either the end of the string or a following "-<backend>" tail. Repoint the base
		// interface, keep the version+backend verbatim.
		if i := strings.LastIndex(bare, "-V"); i > 0 && len(bare) > i+2 {
			j := i + 2
			for j < len(bare) && bare[j] >= '0' && bare[j] <= '9' {
				j++
			}
			if j > i+2 && (j == len(bare) || bare[j] == '-') {
				if nv, ok := rename[bare[:i]]; ok {
					return prefix + nv + bare[i:]
				}
			}
		}
		return s
	}
	// repoint wraps repointBase to strip a trailing Soong output-tag selector before matching. A ref like
	// ":NetworkStackApiStableLib{.jar}" or "service-connectivity-pre-jarjar{.jar}" carries a {<tag>} output
	// selector that is not part of the module name; without splitting it off, the base never matches the
	// rename map and the genrule srcs/tool_files dangle after the lib is renamed. Split, repoint the base,
	// re-append the tag verbatim.
	repoint := func(s string) string {
		if i := strings.IndexByte(s, '{'); i > 0 && strings.HasSuffix(s, "}") {
			return repointBase(s[:i]) + s[i:]
		}
		return repointBase(s)
	}
	// repointProps applies the dep-name / srcs classification to a property list AND descends into
	// nested Map values, re-applying it by property NAME at every depth. A soong_config_module_type
	// carries its dep-name lists a level or more down —
	//   soong_config_variables: { <var>: { defaults: [X], conditions_default: { defaults: [Y] } } }
	// — where X/Y are real module deps the rename must repoint. The top-level-only walk missed them
	// (the observed straggler: framework-telecom-soong-availability-defaults still naming the stock
	// framework-telecom-updatable-defaults after the lane renamed it). Descending by property name is
	// what keeps this safe: only dep/srcs-named lists are repointed, never a blind rewrite of every
	// string in the map (which would corrupt config values / release-flag names).
	var repointProps func(props []*parser.Property)
	repointProps = func(props []*parser.Property) {
		for _, pr := range props {
			switch {
			case depNameProps[pr.Name]:
				// srcsOnly=false: repoint bare names (repoint() itself also handles ":" refs).
				if repointExprRefs(pr.Value, repoint, false) {
					changed = true
				}
			case srcsRefProps[pr.Name]:
				// srcsOnly=true: only ":module" filegroup refs.
				if repointExprRefs(pr.Value, repoint, true) {
					changed = true
				}
			case pr.Name == "cmd" || pr.Name == "flags":
				// genrule cmd + droidstubs flags: repoint ":module"/"//path:module" labels embedded in
				// $(location(s) …) tokens so they stay in sync with the repointed srcs/tools/data. cmd is
				// a "str"+"str" concatenation; flags is a list (and often references top-level vars, handled
				// at assignment level below). Walk the whole expression.
				if repointCmdExpr(pr.Value, repoint) {
					changed = true
				}
			default:
				// Any property can hold ":module" / "//path:module" LABEL refs (data, hdrs, api_file,
				// public_key, key, …) — a label is unambiguously a module reference regardless of the
				// property name, so repoint labels EVERYWHERE (srcsOnly=true leaves plain strings, file
				// paths, flags and config values untouched — only ":"/"//" refs match). This ends the
				// property-by-property whack-a-mole for filegroup/genrule/key refs. Bare-name dep lists
				// still require an explicit depNameProps entry (a bare string is ambiguous).
				if repointExprRefs(pr.Value, repoint, true) {
					changed = true
				}
				switch v := pr.Value.(type) {
				case *parser.Map:
					repointProps(v.Properties)
				case *parser.Select: // select(){ case: {<map>} } — descend into map-valued cases
					for _, cs := range v.Cases {
						if cs != nil {
							if m, ok := cs.Value.(*parser.Map); ok {
								repointProps(m.Properties)
							}
						}
					}
				}
			}
		}
	}
	for _, def := range file.Defs {
		// Top-level variable assignments can hold module-name lists (StubLibraries.bp's
		// non_updatable_api_deps_on_modules = ["DevicePolicyAnnotation", …], later used in `libs:`).
		// Repoint their string leaves too — repoint() only rewrites rename-map keys, so non-module values
		// (paths, flags) are left alone. Skip the reserved subfile directives (build/subdirs), which hold
		// filenames, not module names.
		if as, ok := def.(*parser.Assignment); ok {
			if !blueprintReservedDirectives[as.Name] {
				if repointExprRefs(as.Value, repoint, false) {
					changed = true
				}
				// Top-level vars also hold metalava/genrule flag strings with embedded $(location :X)
				// labels (StubLibraries.bp priv_apps = ["--manifest", "$(location :frameworks-base-core-
				// AndroidManifest.xml)", …], referenced by droidstubs flags:). Repoint those embedded
				// labels too, in lockstep with the renamed source/data modules.
				if repointCmdExpr(as.Value, repoint) {
					changed = true
				}
			}
			continue
		}
		mod, ok := def.(*parser.Module)
		if !ok {
			continue
		}
		// Module identity rename is TOP-LEVEL only (a nested map's `name:` is not a module).
		for _, pr := range mod.Properties {
			if pr.Name != "name" {
				continue
			}
			if ns, ok := pr.Value.(*parser.String); ok {
				if nv, ok := rename[ns.Value]; ok {
					ns.Value = nv
					changed = true
					// aidl_interface in a namespace must set owner (Soong rule).
					if mod.Type == "aidl_interface" && lane != "" {
						setOrAddString(mod, "owner", lane)
					}
				}
			}
		}
		repointProps(mod.Properties)
	}
	if !changed {
		return content, false, nil
	}
	out, err := parser.Print(file)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// dropDepsBp removes named entries from every dep-name list (static_libs/libs/…) and every `:module`
// srcs ref, across all modules in one bp. drop keys are matched exactly (a full label like
// "//frameworks-nexusm/libs/systemui:animationlib", or a bare module name, or a `:name` filegroup ref
// via its bare form). AST-safe via the Blueprint parser; form-preserving. Returns whether it changed.
//
// PRIMARY USE (Model-A no-Compose): the lane's SystemUI component bps were stock-mirrored + delaned, so
// they carry `static_libs: ["//<lane>/libs/systemui:X"]` external-module refs to libs the no-Compose lane
// does NOT vendor outward (the functionality resolves internally via the app's own srcs/Dagger/FQN). Those
// stale external refs point at empty lib dirs → undefined-module. Dropping the ref is the correct fix.
func dropDepsBp(content []byte, drop map[string]bool) ([]byte, bool, error) {
	file, errs := parser.Parse("", bytes.NewReader(content))
	if len(errs) > 0 {
		return nil, false, errs[0]
	}
	shouldDrop := func(s string) bool {
		if drop[s] {
			return true
		}
		return strings.HasPrefix(s, ":") && drop[s[1:]]
	}
	changed := false
	for _, def := range file.Defs {
		mod, ok := def.(*parser.Module)
		if !ok {
			continue
		}
		for _, pr := range mod.Properties {
			if !depNameProps[pr.Name] && !srcsRefProps[pr.Name] {
				continue
			}
			lst, ok := pr.Value.(*parser.List)
			if !ok {
				continue
			}
			kept := lst.Values[:0]
			for _, e := range lst.Values {
				if s, ok := e.(*parser.String); ok && shouldDrop(s.Value) {
					changed = true
					continue
				}
				kept = append(kept, e)
			}
			lst.Values = kept
		}
	}
	if !changed {
		return content, false, nil
	}
	out, err := parser.Print(file)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// runRenameLibs is tier 2: collect the lane lib rename-map, then rename + repoint across the tree.
// RENAME-model only.
func runRenameLibs(c LaneConfig, outRoot string) (changed, failed int) {
	if c.KeepName {
		return 0, 0
	}
	rename := collectLibRenames(outRoot, c)
	fmt.Printf("\nrename libraries (%d lib modules → %s<Name>, repoint deps, AST-safe):\n", len(rename), c.CamelCase)
	if len(rename) == 0 {
		return 0, 0
	}
	for _, root := range []string{"frameworks-" + c.Name, "packages-" + c.Name} {
		rootDir := filepath.Join(outRoot, root)
		if fi, err := os.Stat(rootDir); err != nil || !fi.IsDir() {
			continue
		}
		filepath.Walk(rootDir, func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || !isBpFile(filepath.Base(p)) {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			out, ch, ferr := renameAndRepointBp(b, rename, c.Name)
			if ferr != nil {
				failed++
				return nil
			}
			if ch {
				os.WriteFile(p, out, 0o644)
				changed++
			}
			return nil
		})
	}
	fmt.Printf("  %d bp updated, %d skipped (parse).\n", changed, failed)
	return changed, failed
}

// runRenameInstallables walks the lane's cloned frameworks-<lane>/ + packages-<lane>/ trees and
// applies the installable rename. Only runs for the RENAME model (KeepName=false).
func runRenameInstallables(c LaneConfig, outRoot string) (changed, failed int) {
	if c.KeepName {
		return 0, 0
	}
	fmt.Printf("\nrename installables (name → %s<Name> + overrides, AST-safe):\n", c.CamelCase)
	for _, root := range []string{"frameworks-" + c.Name, "packages-" + c.Name} {
		rootDir := filepath.Join(outRoot, root)
		if fi, err := os.Stat(rootDir); err != nil || !fi.IsDir() {
			continue
		}
		filepath.Walk(rootDir, func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || !isBpFile(filepath.Base(p)) {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			out, ch, ferr := renameInstallableBp(b, c.CamelCase)
			if ferr != nil {
				failed++
				return nil
			}
			if ch {
				os.WriteFile(p, out, 0o644)
				changed++
			}
			return nil
		})
	}
	fmt.Printf("  %d bp renamed, %d skipped (parse).\n", changed, failed)
	return changed, failed
}
