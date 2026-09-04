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
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
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
		if !srcsOnly || strings.HasPrefix(v.Value, ":") {
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
	repoint := func(s string) string {
		if nv, ok := rename[s]; ok { // bare dep name
			return nv
		}
		if strings.HasPrefix(s, ":") { // same-package filegroup ref
			if nv, ok := rename[s[1:]]; ok {
				return ":" + nv
			}
		}
		return s
	}
	for _, def := range file.Defs {
		mod, ok := def.(*parser.Module)
		if !ok {
			continue
		}
		for _, pr := range mod.Properties {
			switch {
			case pr.Name == "name":
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
			}
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
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
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
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
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
