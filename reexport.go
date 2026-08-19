// Copyright (C) 2026 The Android Open Source Project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

// reexport.go — the RE-EXPORT idiom (app-naming / Model-A). When the finder drops a full-REPLACEMENT
// identity app (its stock subtree is replaced by the lane's NexusM<App> fork), the KEEP-NAME modules
// that app EXPORTED — shared java_defaults, resource libs, plugin/statsd/flags libs, `*-core`/`*-res` —
// vanish from the graph. Un-forked / cross-namespace consumers that still reference them by keep-name
// (Launcher3, SystemUIGo, tracinglib, automotive, dev samples; all typically non-phone or
// -no-compose-excluded) then fail to resolve. This pass AST-detects those orphaned keep-name exports
// that are STILL dep-referenced by the surviving graph and emits minimal keep-name re-export stubs into
// frameworks-<lane>/base/shared-app-defaults/Android.bp (a frameworks-<lane> location that collapses to
// the global namespace on a lane lunch, so the stubs resolve for both root-ns stock and lane consumers).
// It replaces hand-authoring those stubs one build-error at a time.

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	parser "github.com/abstractsrevenge/sovereign_lane_surgeon/internal/blueprint/parser"
)

// cmdReexport is the `reexport` subcommand: auto-generate keep-name re-export stubs for symbols
// orphaned by the lane's replaced identity apps. Dry-run by default; -apply writes the bp.
func cmdReexport(args []string) int {
	fs := flag.NewFlagSet("reexport", flag.ExitOnError)
	name := fs.String("name", "", "lane name (lowercase, e.g. nexusm)")
	out := fs.String("out", "", "AOSP root")
	prefixDirs := fs.String("prefix-dirs", "", "identity-app dir/module prefix (e.g. NexusM) — required (rename model)")
	apply := fs.Bool("apply", false, "write the re-export bp (default: dry-run plan)")
	_ = fs.Parse(args)
	if *name == "" || *out == "" || *prefixDirs == "" {
		fmt.Fprintln(os.Stderr, "reexport: -name, -out and -prefix-dirs are required")
		return 2
	}
	c := deriveLane(*name, false, nil, false, false, *prefixDirs)
	runReexport(c, *out, *apply)
	return 0
}

// bpModules parses a bp and returns its top-level modules (name -> type) and whether it declares an
// APEX shared-stub type (java_sdk_library/aidl_interface/bootstrap_go_package — Soong-derived names that
// stay shared-stock and mark their whole subtree non-droppable).
func bpModules(path string) (names map[string]string, apex bool) {
	names = map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return names, false
	}
	file, errs := parser.Parse("", bytes.NewReader(b))
	if len(errs) > 0 {
		return names, false
	}
	for _, def := range file.Defs {
		mod, ok := def.(*parser.Module)
		if !ok {
			continue
		}
		switch mod.Type {
		case "java_sdk_library", "aidl_interface", "bootstrap_go_package":
			apex = true
		}
		for _, p := range mod.Properties {
			if p.Name == "name" {
				if s, ok := p.Value.(*parser.String); ok && s.Value != "" {
					names[s.Value] = mod.Type
				}
			}
		}
	}
	return names, apex
}

// subtreeHasApex reports whether any bp under dir declares an apex shared-stub type.
func subtreeHasApex(dir string) bool {
	found := false
	filepath.Walk(dir, func(p string, fi os.FileInfo, e error) error {
		if e != nil || found || fi.IsDir() || filepath.Base(p) != "Android.bp" {
			return nil
		}
		if _, apex := bpModules(p); apex {
			found = true
		}
		return nil
	})
	return found
}

// stubAttrs carries the SDK/apex-compat attributes copied from an orphaned module's ORIGINAL definition.
// A keep-name stub must reproduce them so it stays usable by UPDATABLE/mainline consumers — an updatable
// app (e.g. CarDocumentsUI, updatable:true) requires its defaults to set sdk_version+min_sdk_version and
// its static_libs to support min_sdk_version; an attribute-empty stub strips that and fails analysis
// ("updatable apps must set min_sdk_version" / "should support min_sdk_version(N)"). Empty fields are
// omitted from the emitted stub.
type stubAttrs struct {
	sdkVersion    string
	minSdkVersion string
	apexAvailable []string
}

// bpModuleAttrs parses a bp and returns each module's sdk_version / min_sdk_version / apex_available so a
// re-export stub can mirror the ORIGINAL (finder-dropped, still-on-disk) module's SDK posture.
func bpModuleAttrs(path string) map[string]stubAttrs {
	res := map[string]stubAttrs{}
	b, err := os.ReadFile(path)
	if err != nil {
		return res
	}
	file, errs := parser.Parse("", bytes.NewReader(b))
	if len(errs) > 0 {
		return res
	}
	for _, def := range file.Defs {
		mod, ok := def.(*parser.Module)
		if !ok {
			continue
		}
		var name string
		var a stubAttrs
		for _, p := range mod.Properties {
			switch p.Name {
			case "name":
				if s, ok := p.Value.(*parser.String); ok {
					name = s.Value
				}
			case "sdk_version":
				if s, ok := p.Value.(*parser.String); ok {
					a.sdkVersion = s.Value
				}
			case "min_sdk_version":
				if s, ok := p.Value.(*parser.String); ok {
					a.minSdkVersion = s.Value
				}
			case "apex_available":
				if l, ok := p.Value.(*parser.List); ok {
					for _, el := range l.Values {
						if s, ok := el.(*parser.String); ok && s.Value != "" {
							a.apexAvailable = append(a.apexAvailable, s.Value)
						}
					}
				}
			}
		}
		if name != "" {
			res[name] = a
		}
	}
	return res
}

// bpDeclaresAllPrefixed reports whether every module name in a bp starts with the CamelCase shared-infra
// prefix (e.g. "Nexusm"). Such a root is ADDITIVE (keep stock); a bp with ANY name not so prefixed —
// INCLUDING a DirPrefix-named identity module (NexusMCar, capital-M, which is NOT a "Nexusm" prefix) or
// a genuine keep-name module — is a REPLACEMENT. This mirrors the finder's nexusmForkKeepsStock EXACTLY
// (CamelCase-only), so the reexport's dropped-subtree set matches what the finder actually drops.
func bpDeclaresAllPrefixed(path, camel string) bool {
	names, _ := bpModules(path)
	if len(names) == 0 {
		return false
	}
	for n := range names {
		if !strings.HasPrefix(n, camel) {
			return false
		}
	}
	return true
}

// bpDeclaresOverrides reports whether any module in a bp declares an `overrides:` property — the
// definitive install-REPLACEMENT signal. Mirrors the finder's nexusmForkKeepsStock overrides check: the
// lane's identity apps are named lowercase-`Nexusm<Name>` (so bpDeclaresAllPrefixed reads them as
// additive) yet `overrides: ["<Stock>"]` their donor, so the finder DROPS their stock parallel. The
// re-export's dropped-subtree set must agree, or the newly-dropped subtrees' orphaned keep-name exports
// go unstubbed and their cross-namespace consumers break.
func bpDeclaresOverrides(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	file, errs := parser.Parse("", bytes.NewReader(b))
	if len(errs) > 0 {
		return false
	}
	for _, def := range file.Defs {
		if mod, ok := def.(*parser.Module); ok {
			for _, p := range mod.Properties {
				if p.Name == "overrides" {
					return true
				}
			}
		}
	}
	return false
}

// deprefixIdentitySegment reverses the DirPrefix on the first prefixed path segment (NexusMFoo -> Foo).
func deprefixIdentitySegment(path, dirPrefix string) string {
	if dirPrefix == "" {
		return path
	}
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, dirPrefix) && len(s) > len(dirPrefix) {
			segs[i] = strings.TrimPrefix(s, dirPrefix)
			return strings.Join(segs, "/")
		}
	}
	return path
}

// droppedStockSubtrees returns the de-prefixed stock subtree dirs (relative to outRoot) that the finder
// DROPS: NexusM<App> identity-app roots that are REPLACEMENTS (not additive) and NOT apex-module
// subtrees. Their exported keep-name modules are the re-export candidates.
func droppedStockSubtrees(c LaneConfig, outRoot string) []string {
	if c.DirPrefix == "" {
		return nil
	}
	seen := map[string]bool{}
	for _, root := range []string{"frameworks-" + c.Name, "packages-" + c.Name} {
		laneRoot := filepath.Join(outRoot, root)
		filepath.Walk(laneRoot, func(p string, fi os.FileInfo, e error) error {
			if e != nil || !fi.IsDir() {
				return nil
			}
			base := filepath.Base(p)
			if !strings.HasPrefix(base, c.DirPrefix) || len(base) == len(c.DirPrefix) {
				return nil
			}
			// p is an identity-app root dir. Only consider the OUTERMOST one (skip nested).
			rel, _ := filepath.Rel(outRoot, p)
			rootBp := filepath.Join(p, "Android.bp")
			// Match the finder's nexusmForkKeepsStock EXACTLY: a MISSING root bp (a container dir like
			// NexusMCar with no own Android.bp) reads as false → REPLACEMENT (drops the whole stock
			// subtree). An EXISTING all-"Nexusm"-prefixed root is additive (keep stock) UNLESS it declares
			// `overrides:` — the install-replacement signal the lane's lowercase-Nexusm identity apps use.
			if _, err := os.Stat(rootBp); err == nil && bpDeclaresAllPrefixed(rootBp, c.CamelCase) && !bpDeclaresOverrides(rootBp) {
				return filepath.SkipDir // additive fork -> stock kept
			}
			// de-prefix the lane identity dir -> stock dir
			var sdir string
			if strings.HasPrefix(rel, "frameworks-"+c.Name+"/") {
				sdir = "frameworks/" + strings.TrimPrefix(rel, "frameworks-"+c.Name+"/")
			} else if strings.HasPrefix(rel, "packages-"+c.Name+"/") {
				sdir = "packages/" + strings.TrimPrefix(rel, "packages-"+c.Name+"/")
			} else {
				return filepath.SkipDir
			}
			sdir = deprefixIdentitySegment(sdir, c.DirPrefix)
			stockDir := filepath.Join(outRoot, sdir)
			if fi2, err := os.Stat(stockDir); err != nil || !fi2.IsDir() {
				return filepath.SkipDir
			}
			if subtreeHasApex(stockDir) {
				return filepath.SkipDir // apex module subtree stays shared-stock
			}
			seen[sdir] = true
			return filepath.SkipDir // don't descend into nested identity dirs
		})
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// laneBpDeclaresNamespace reports whether a bp declares a soong_namespace module (mirrors the finder's
// bpDeclaresNamespace). The finder collapses such a lane bp to root and never drops a stock parallel
// against it, so the re-export must not treat it as dropping stock either.
func laneBpDeclaresNamespace(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	file, errs := parser.Parse("", bytes.NewReader(b))
	if len(errs) > 0 {
		return false
	}
	for _, def := range file.Defs {
		if mod, ok := def.(*parser.Module); ok && mod.Type == "soong_namespace" {
			return true
		}
	}
	return false
}

// laneForkKeepsStock mirrors the finder's nexusmForkKeepsStock: a lane bp is ADDITIVE (its stock parallel
// is KEPT) iff every module name is prefixed AND it declares no `overrides:`. Any keep-name (non-prefixed)
// twin or an `overrides:` makes it a REPLACEMENT whose stock parallel the finder drops.
func laneForkKeepsStock(path string, c LaneConfig) bool {
	return bpDeclaresAllPrefixed(path, c.CamelCase) && !bpDeclaresOverrides(path)
}

// stockParallelDropped mirrors the finder's existingStockParallelNexusm PER-FILE decision: given a lane bp
// (relative to outRoot), returns the stock parallel bp the finder DROPS, or "" if none. This is what lets
// an additive-ROOT library fork (NexusMSettingsLib) still drop the stock parallels of its keep-name-twin
// subdirs (tests/flags/apex) — the finder decides per-file, so the re-export must too, or it miscounts
// those dropped keep-name modules as still-defined and fails to stub the orphans stock consumers need.
func stockParallelDropped(laneRel string, c LaneConfig, outRoot string) string {
	laneBp := filepath.Join(outRoot, laneRel)
	// A namespace-decl lane bp: the finder drops the LANE bp (collapse to root) and KEEPS the stock
	// parallel — it never runs a stock-parallel drop against it. So it drops no stock (e.g. the lane's
	// frameworks-<lane>/base/packages/Android.bp must NOT be read as dropping stock's
	// frameworks/base/packages/Android.bp, which defines platform_app_defaults).
	if laneBpDeclaresNamespace(laneBp) {
		return ""
	}
	if laneForkKeepsStock(laneBp, c) {
		return "" // additive → stock kept
	}
	var rel string
	if strings.HasPrefix(laneRel, "frameworks-"+c.Name+"/") {
		rel = "frameworks/" + strings.TrimPrefix(laneRel, "frameworks-"+c.Name+"/")
	} else if strings.HasPrefix(laneRel, "packages-"+c.Name+"/") {
		rel = "packages/" + strings.TrimPrefix(laneRel, "packages-"+c.Name+"/")
	} else {
		return ""
	}
	if fi, err := os.Stat(filepath.Join(outRoot, rel)); err == nil && !fi.IsDir() {
		return rel // direct tree-swap parallel (framework-class / shared infra)
	}
	if dp := deprefixIdentitySegment(rel, c.DirPrefix); dp != rel {
		if fi, err := os.Stat(filepath.Join(outRoot, dp)); err == nil && !fi.IsDir() {
			return dp // identity de-prefix parallel
		}
	}
	return ""
}

// droppedStockParallelBps returns the set of stock bp paths (relative to outRoot) the finder drops PER-FILE
// (mirroring stockParallelDropped over every lane bp). Complements droppedStockSubtrees (whole identity
// subtrees): together they are the finder's full drop set, which the re-export's defined/orphaned scan must
// match exactly.
func droppedStockParallelBps(c LaneConfig, outRoot string) map[string]bool {
	out := map[string]bool{}
	for _, root := range []string{"frameworks-" + c.Name, "packages-" + c.Name} {
		filepath.Walk(filepath.Join(outRoot, root), func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
				return nil
			}
			rel, _ := filepath.Rel(outRoot, p)
			if s := stockParallelDropped(rel, c, outRoot); s != "" {
				out[s] = true
			}
			return nil
		})
	}
	return out
}

// depNamesInBp collects every bare-name / :name dep referenced by a bp's dep-name + srcs-ref lists.
func depNamesInBp(path string, into map[string]bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	file, errs := parser.Parse("", bytes.NewReader(b))
	if len(errs) > 0 {
		return
	}
	// walkExpr recursively collects :module / bare-name refs from an expression — handling not just a
	// plain list but LIST + SELECT/OPERATOR CONCATENATIONS (e.g. device_common_srcs: [...] + select(...)),
	// which is how the framework source aggregator references :PacProcessor-aidl-sources et al. Missing
	// the Operator/Select recursion silently drops every ref past the first list (referenced=false).
	var walkExpr func(parser.Expression)
	walkExpr = func(e parser.Expression) {
		switch v := e.(type) {
		case *parser.String:
			s := strings.TrimPrefix(v.Value, ":")
			if s != "" && !strings.HasPrefix(s, "//") {
				into[s] = true
			}
		case *parser.List:
			for _, el := range v.Values {
				walkExpr(el)
			}
		case *parser.Operator:
			walkExpr(v.Args[0])
			walkExpr(v.Args[1])
		case *parser.Select:
			for _, cs := range v.Cases {
				if cs != nil {
					walkExpr(cs.Value)
				}
			}
			if v.Append != nil {
				walkExpr(v.Append)
			}
		}
	}
	// isDepRefProp reports whether a property's value carries module refs a re-export must satisfy.
	isDepRefProp := func(name string) bool {
		return depNameProps[name] || srcsRefProps[name] ||
			name == "jni_libs" || name == "plugins" || name == "data" || name == "extra_check_modules"
	}
	// walkContainers descends structurally through nested Maps / Lists WITHOUT collecting leaf strings —
	// its only job is to REACH dep props nested inside conditional/variant blocks (soong_config_variables,
	// arch, target, product_variables, lint, multilib, …) at ANY depth, e.g.
	// soong_config_variables: { release_x: { srcs: [":mod"] } }. Missing this recursion silently drops
	// every dep referenced only under such a conditional (referenced=false → the re-export never stubs it).
	var walkContainers func(parser.Expression)
	var walkProps func([]*parser.Property)
	walkContainers = func(e parser.Expression) {
		switch v := e.(type) {
		case *parser.Map:
			walkProps(v.Properties)
		case *parser.List:
			for _, el := range v.Values {
				walkContainers(el)
			}
		}
	}
	walkProps = func(props []*parser.Property) {
		for _, pr := range props {
			switch {
			case pr.Name == "instrumentation_for":
				// String-valued module ref (names the app-under-test — a real dep).
				if s, ok := pr.Value.(*parser.String); ok && s.Value != "" {
					into[s.Value] = true
				}
			case isDepRefProp(pr.Name):
				walkExpr(pr.Value)
			default:
				// Not a dep prop itself — descend in case it CONTAINS one (nested conditional/variant map).
				walkContainers(pr.Value)
			}
		}
	}
	for _, def := range file.Defs {
		if mod, ok := def.(*parser.Module); ok {
			walkProps(mod.Properties)
		}
	}
}

// scanSurviving walks all stock frameworks/+packages/ bps that are NOT under a dropped subtree and
// collects (a) every dep name referenced and (b) every module name defined. A re-export is needed for
// an orphaned keep-name module that is referenced but not (re)defined in the surviving graph.
// rootVisibleDef reports whether a bp at rel (relative to outRoot) defines modules in the GLOBAL
// namespace on a lane lunch: stock trees, or lane framework-class (frameworks-<lane>/ outside
// base/packages/, which the finder collapses to root). Lane namespaced trees (packages-<lane>/,
// frameworks-<lane>/base/packages/) are NOT root-visible.
func rootVisibleDef(rel, lane string) bool {
	if !strings.HasPrefix(rel, "frameworks-"+lane) && !strings.HasPrefix(rel, "packages-"+lane) {
		return true // stock
	}
	// Lane content collapses to the global namespace (finder drops its namespace decls, Holo-style) EXCEPT
	// the decomposition pods (frameworks-<lane>/base/packages/*/pods/**), whose short colliding module
	// names (api/impl repeated across domain/data layers) keep a nested namespace. So ALL other lane
	// content — packages-<lane>/**, frameworks-<lane>/ framework-class, and the base/packages identity
	// apps + shared libs (SettingsLib, the apex forks) — is root-visible and needs NO re-export stub;
	// it claims the canonical root slot directly, exactly as Holo's fully-collapsed lane does.
	if strings.Contains(rel, "/pods/") {
		return false // pod nested namespace retained
	}
	return true // collapses to root
}

// survivingSourceRoots enumerates the top-level source dirs to scan for consumers/definitions: every
// dir with Android.bp EXCEPT build outputs / prebuilts / vcs / toolchains, and EXCEPT sibling lanes
// (a nexusm build is blind to *-holo / *-nexus / *-product, mirroring the finder's other-lane drop —
// but keeps its own *-<lane>). Consumers of an orphaned keep-name export can live anywhere
// (build/make's generic system image, development/, cts/, external/, …), not just frameworks/+packages/.
func survivingSourceRoots(outRoot, lane string) []string {
	entries, err := os.ReadDir(outRoot)
	if err != nil {
		return []string{"frameworks", "packages", "frameworks-" + lane, "packages-" + lane}
	}
	var roots []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		n := e.Name()
		// Skip build outputs, prebuilts, toolchains, and any hidden/underscore dir — crucially
		// _snapshots/ (the surgeon's own backups, full of stock bp COPIES that would pollute `defined`
		// and wrongly suppress re-exports).
		switch {
		case strings.HasPrefix(n, "out"), strings.HasPrefix(n, "."), strings.HasPrefix(n, "_"),
			n == "prebuilts", n == "kernel", n == "toolchain":
			continue
		}
		// sibling lanes (not this lane) — the nexusm build is unaware of them.
		if n != "frameworks-"+lane && n != "packages-"+lane && n != "external-"+lane && n != "device" {
			if strings.HasSuffix(n, "-holo") || strings.HasSuffix(n, "-nexus") || strings.HasSuffix(n, "-product") {
				continue
			}
		}
		roots = append(roots, n)
	}
	return roots
}

func scanSurviving(outRoot, lane string, droppedRel []string, droppedBps map[string]bool) (referenced, defined map[string]bool) {
	referenced, defined = map[string]bool{}, map[string]bool{}
	isDropped := func(rel string) bool {
		for _, d := range droppedRel {
			if rel == d || strings.HasPrefix(rel, d+"/") {
				return true
			}
		}
		return false
	}
	// Walk BOTH stock frameworks/+packages/ AND the lane trees — a lane consumer (NexusMPrintSpooler,
	// NexusmSampleDvbTuner) that references an orphaned keep-name export by name is just as real a
	// reference as a stock one; missing the lane roots misses those (jni_libs etc.).
	ownOutput := filepath.Join("frameworks-"+lane, "base", "shared-app-defaults")
	for _, top := range survivingSourceRoots(outRoot, lane) {
		filepath.Walk(filepath.Join(outRoot, top), func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
				return nil
			}
			rel, _ := filepath.Rel(outRoot, p)
			// Skip whole dropped identity subtrees (dir-prefix) AND per-file dropped stock parallels
			// (exact bp path — a per-file drop removes ONLY that Android.bp, not its subdirs).
			if isDropped(filepath.Dir(rel)) || droppedBps[rel] {
				return nil
			}
			// Skip the reexport's OWN generated output — reading it back as "defined" would make the
			// pass non-idempotent (a second run drops everything the first run already emitted).
			if strings.HasPrefix(filepath.Dir(rel), ownOutput) {
				return nil
			}
			// referenced: from EVERY surviving bp (stock + lane consumers alike).
			depNamesInBp(p, referenced)
			// defined: only ROOT-VISIBLE definitions. Stock (root) and lane framework-class
			// (frameworks-<lane>/ NOT under base/packages/, which collapses to the global namespace)
			// count; lane NAMESPACED defs (packages-<lane>/**, frameworks-<lane>/base/packages/**) do
			// NOT — a keep-name module defined only there is invisible to a root-ns consumer and still
			// needs a root-visible re-export (e.g. com_android_systemui_flags_lib in NexusMSystemUI/aconfig).
			if rootVisibleDef(rel, lane) {
				names, _ := bpModules(p)
				for n := range names {
					defined[n] = true
				}
			}
			return nil
		})
	}
	return referenced, defined
}

// stubForType emits a minimal keep-name stub module for a given original module type. Returns "" for
// types not worth stubbing (they will not be re-exported).
// isInstallableType reports whether a module type is an installable (app or binary) that a system
// image / PRODUCT_PACKAGES references by name through product config (.mk) — invisible to a .bp scan.
// Such a dropped module's keep-name is stubbed unconditionally.
func isInstallableType(typ string) bool {
	switch typ {
	case "android_app", "android_app_import", "java_binary", "cc_binary", "sh_binary", "rust_binary":
		return true
	}
	return false
}

func stubForType(name, typ string, a stubAttrs) string {
	// sdkBlock builds the SDK/apex-compat attribute lines a stub must carry so it stays consumable by an
	// updatable/mainline module. sdk_version, min_sdk_version and apex_available are copied from the
	// ORIGINAL dropped def (attribute-faithful); when the original set no sdk_version and one is required
	// by the stub type, defSdk is the fallback. wantSdk=false suppresses sdk_version entirely (defaults may
	// legitimately carry none; native libs take no sdk_version).
	sdkBlock := func(defSdk string, wantSdk bool) string {
		var sb strings.Builder
		if wantSdk {
			sdk := a.sdkVersion
			if sdk == "" {
				sdk = defSdk
			}
			if sdk != "" {
				fmt.Fprintf(&sb, "    sdk_version: %q,\n", sdk)
			}
		}
		if a.minSdkVersion != "" {
			fmt.Fprintf(&sb, "    min_sdk_version: %q,\n", a.minSdkVersion)
		}
		if len(a.apexAvailable) > 0 {
			qs := make([]string, len(a.apexAvailable))
			for i, s := range a.apexAvailable {
				qs[i] = fmt.Sprintf("%q", s)
			}
			fmt.Fprintf(&sb, "    apex_available: [%s],\n", strings.Join(qs, ", "))
		}
		return sb.String()
	}
	// Any *_defaults type (incl. soong_config custom types like systemui_optimized_java_defaults) is
	// consumed via `defaults:` — it MUST be stubbed as a java_defaults, not a java_library. Carry the
	// original's sdk_version+min_sdk_version+apex_available so consumers that INHERIT their SDK posture
	// from this defaults (e.g. an updatable app whose sdk_version/min_sdk_version come from here) keep it.
	if strings.Contains(typ, "defaults") {
		return fmt.Sprintf("java_defaults {\n    name: %q,\n%s}\n", name, sdkBlock("", true))
	}
	switch typ {
	case "java_defaults":
		return fmt.Sprintf("java_defaults {\n    name: %q,\n%s}\n", name, sdkBlock("", true))
	case "java_library_host", "java_test_host", "java_plugin":
		// HOST modules (e.g. a lint checker consumed via lint.extra_check_modules) need the host variant.
		return fmt.Sprintf("java_library_host {\n    name: %q,\n}\n", name)
	case "java_library", "java_library_static", "java_sdk_library_import", "java_import", "java_genrule", "java_binary", "sh_binary", "rust_binary":
		// host_supported so a HOST consumer (annotation processors, javapoet imports, ...) finds the
		// host variant too; a device-only consumer just uses the device variant.
		return fmt.Sprintf("java_library {\n    name: %q,\n%s    host_supported: true,\n}\n", name, sdkBlock("core_current", true))
	case "android_library", "android_library_import":
		// use_resource_processor:false = legacy aapt2 path, which exposes the `{.aapt.srcjar}` (R.java
		// srcjar) output tag that robolectric-stub consumers reference; the modern processor emits
		// `{.aapt.jar}` instead and leaves aaptSrcJar nil ("unsupported module reference tag .aapt.srcjar").
		// Harmless on a resource-less stub.
		return fmt.Sprintf("android_library {\n    name: %q,\n%s    manifest: \"AndroidManifest.xml\",\n    use_resource_processor: false,\n}\n", name, sdkBlock("system_current", true))
	case "android_app", "android_app_import", "android_test", "android_test_helper_app":
		// instrumentation_for names the app-under-test — must be an android_app. android_app_import (a
		// prebuilt app like CtsShimPrebuilt referenced by PRODUCT_PACKAGES) must ALSO stub as a DEVICE
		// android_app, NOT the default host_supported java_library — a host module in PRODUCT_PACKAGES is a
		// kati error ("Host modules should be in PRODUCT_HOST_PACKAGES"). use_resource_processor:false so a
		// `{.aapt.srcjar}` reference resolves.
		// use_resource_processor:false so a `{.aapt.srcjar}` reference resolves. (These keep-name app stubs
		// are installable like Holo's lane apps; the lane device mk permits their artifact paths via
		// PRODUCT_ARTIFACT_PATH_REQUIREMENT_ALLOWED_LIST, mirroring Holo.)
		return fmt.Sprintf("android_app {\n    name: %q,\n%s    manifest: \"AndroidManifest.xml\",\n    use_resource_processor: false,\n}\n", name, sdkBlock("system_current", true))
	case "filegroup":
		// A filegroup consumed via privapp_allowlist: (or any single-file XML consumer) must produce
		// EXACTLY ONE file — an empty filegroup fails "produced no files, expected exactly one". Reference
		// a shared placeholder empty-allowlist XML (written by runReexport). Generic filegroups (java srcs,
		// aidl) stay empty — a placeholder XML there would be a spurious source.
		if strings.Contains(name, "privapp_allowlist") || strings.HasSuffix(name, ".xml") {
			return fmt.Sprintf("filegroup {\n    name: %q,\n    srcs: [%q],\n}\n", name, reexportPlaceholderXML)
		}
		return fmt.Sprintf("filegroup {\n    name: %q,\n}\n", name)
	case "python_library", "python_library_host", "python_binary_host", "python_test_host", "python_defaults":
		// Python deps (e.g. a dropped Nfc testutils' pn532-python consumed by a CTS host test). A
		// java_library stub has no PY3 variant ("missing variant: linux_glibc_x86_64_PY3"); a
		// python_library_host provides the host PY3 variant a test consumer needs.
		return fmt.Sprintf("python_library_host {\n    name: %q,\n}\n", name)
	case "cc_library", "cc_library_shared", "cc_library_static", "cc_library_headers", "cc_binary":
		// JNI / native libs referenced via jni_libs etc. A header-only cc_library_shared analyses
		// cleanly (no srcs required) and satisfies the dep for graph coherence. sdk_version is REQUIRED
		// here (wantSdk=true): a native dep consumed by an sdk/NDK-linked app (e.g. libshim_jni in the
		// updatable CtsShim priv-app) needs the `sdk:sdk` variant, which cc only produces when the lib
		// itself sets sdk_version — without it the consumer reports "missing variant os:android,…,sdk:sdk".
		// min_sdk_version/apex_available likewise carried for apex/updatable native consumers.
		return fmt.Sprintf("cc_library_shared {\n    name: %q,\n%s    system_shared_libs: [],\n    stl: \"none\",\n    host_supported: true,\n}\n", name, sdkBlock("", true))
	case "genrule":
		return "" // generated outputs — cannot stub meaningfully; leave to a manual port
	default:
		// Unknown/binary types: emit a host_supported java_library stub (satisfies a static_libs/libs
		// ref for graph analysis, device OR host).
		return fmt.Sprintf("java_library {\n    name: %q,\n%s    host_supported: true,\n}\n", name, sdkBlock("core_current", true))
	}
}

// reexportPlaceholderXML is the shared one-file placeholder an allowlist/XML filegroup stub references
// so a privapp_allowlist: consumer gets exactly one file.
const reexportPlaceholderXML = "empty_privapp_allowlist.xml"

// runReexport is the entry point. apply=false prints the plan; apply=true writes the re-export bp.
func runReexport(c LaneConfig, outRoot string, apply bool) {
	if c.DirPrefix == "" {
		fmt.Println("reexport: only meaningful for the rename/app-naming model (needs -prefix-dirs); nothing to do.")
		return
	}
	dropped := droppedStockSubtrees(c, outRoot)
	fmt.Printf("reexport: %d dropped replacement identity-app stock subtrees:\n", len(dropped))
	for _, d := range dropped {
		fmt.Printf("  - %s\n", d)
	}
	droppedBps := droppedStockParallelBps(c, outRoot)
	fmt.Printf("reexport: %d per-file dropped stock parallels (keep-name-twin subdirs of additive forks)\n", len(droppedBps))
	orphaned := map[string]string{} // keep-name module -> type
	attrs := map[string]stubAttrs{} // keep-name module -> SDK/apex attributes of its ORIGINAL def
	collectOrphans := func(p string) {
		mods, _ := bpModules(p)
		ma := bpModuleAttrs(p)
		for n, t := range mods {
			if !strings.HasPrefix(n, c.CamelCase) && (c.DirPrefix == "" || !strings.HasPrefix(n, c.DirPrefix)) {
				orphaned[n] = t // keep-name export
				attrs[n] = ma[n]
			}
		}
	}
	for _, sub := range dropped {
		filepath.Walk(filepath.Join(outRoot, sub), func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
				return nil
			}
			collectOrphans(p)
			return nil
		})
	}
	// Per-file dropped stock parallels: their keep-name modules are orphaned too (the finder drops the
	// bp, so stock/un-forked consumers referencing those keep-names by bare name need a re-export).
	for bpRel := range droppedBps {
		collectOrphans(filepath.Join(outRoot, bpRel))
	}
	referenced, defined := scanSurviving(outRoot, c.Name, dropped, droppedBps)
	needed := map[string]string{}
	for name, typ := range orphaned {
		if defined[name] {
			continue
		}
		// Installable modules (apps + binaries) are referenced by PRODUCT_PACKAGES and system-image
		// modules through PRODUCT CONFIG (.mk) — invisible to a .bp AST scan — so a dropped installable's
		// own keep-name is stubbed whether or not a .bp reference was found. This matches how stock and
		// Holo (keep-name) satisfy the generic aosp_shared_system_image: the module simply exists by
		// keep-name. The lane's renamed module still installs via overrides:[]; the stub is graph-coherence
		// for images the lane product itself does not build. Everything else needs a real .bp reference.
		if referenced[name] || isInstallableType(typ) {
			needed[name] = typ
		}
	}
	names := make([]string, 0, len(needed))
	for n := range needed {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Printf("reexport: %d orphaned keep-name exports referenced by the surviving graph → re-export:\n", len(names))

	var sb strings.Builder
	sb.WriteString("// AUTO-GENERATED by `sovereign-lane-surgeon reexport`. Keep-name re-export stubs for modules\n")
	sb.WriteString("// exported by replaced identity apps and still referenced by un-forked/cross-namespace\n")
	sb.WriteString("// consumers. frameworks-" + c.Name + "/base/ collapses to the global namespace so these resolve\n")
	sb.WriteString("// for both root-ns stock and lane consumers. Minimal-for-graph-coherence. Regenerate; do not hand-edit.\n\n")
	sb.WriteString("package {\n    default_applicable_licenses: [\"Android-Apache-2.0\"],\n}\n\n")
	needManifest := false
	needPlaceholderXML := false
	for _, n := range names {
		s := stubForType(n, needed[n], attrs[n])
		if s == "" {
			fmt.Printf("  ! %-45s (type %s) — not auto-stubbable, port manually\n", n, needed[n])
			continue
		}
		if strings.Contains(s, "android_library") {
			needManifest = true
		}
		if strings.Contains(s, reexportPlaceholderXML) {
			needPlaceholderXML = true
		}
		fmt.Printf("  + %-45s (type %s)\n", n, needed[n])
		sb.WriteString(s)
		sb.WriteString("\n")
	}
	if !apply {
		fmt.Println("\n(dry-run — pass -apply to write frameworks-" + c.Name + "/base/shared-app-defaults/Android.bp)")
		return
	}
	dir := filepath.Join(outRoot, "frameworks-"+c.Name, "base", "shared-app-defaults")
	os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, "Android.bp"), []byte(sb.String()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "reexport: write bp: %v\n", err)
		return
	}
	if needManifest {
		mf := "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<manifest xmlns:android=\"http://schemas.android.com/apk/res/android\"\n    package=\"com.android." + c.Name + ".reexport\">\n</manifest>\n"
		os.WriteFile(filepath.Join(dir, "AndroidManifest.xml"), []byte(mf), 0o644)
	}
	if needPlaceholderXML {
		// Empty but valid privapp permission allowlist — one file, so a privapp_allowlist: filegroup stub
		// produces exactly one file. Grants no permissions (a stub for an un-forked stock consumer's dep).
		ph := "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<permissions>\n</permissions>\n"
		os.WriteFile(filepath.Join(dir, reexportPlaceholderXML), []byte(ph), 0o644)
	}
	fmt.Printf("\nreexport: wrote %d stubs to frameworks-%s/base/shared-app-defaults/Android.bp\n", len(names), c.Name)
}
