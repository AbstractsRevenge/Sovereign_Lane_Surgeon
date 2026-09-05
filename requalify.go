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
	"regexp"
	"strings"

	parser "github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/internal/blueprint/parser"
)

// requalify.go — §23.1 requalifier. When a lane forks a large subtree (e.g. frameworks/base), the
// finder's keep-name per-file replacement drops each stock parallel bp, which breaks every
// fully-qualified INTERNAL label (`//frameworks/base/X:Mod`) that pointed at the stock path — the
// module now lives at `//frameworks-<lane>/base/X:Mod`. This rewrites those labels to the lane
// path, PRESERVING the `//path:module` form (a repoint, NOT a bareify — sidesteps the §11-12
// bareify SRCS breakage). Fork-boundary aware: a label is only repointed if its target dir was
// actually forked (exists under the lane root) — so unforked subtrees (which fell through to stock)
// keep their stock labels. AST-safe via the vendored blueprint parser (HARD RULE 3 — no regex).

// requalifyLabel repoints a single `//<root>/<path>:<mod>` label to the lane root when the target
// dir is forked. laneMap maps a stock root ("frameworks") to its lane root ("frameworks-<lane>").
// applyRenamePrefix prefixes the app/package segment for the rename model, e.g.
// packages-nexusm/apps/Nfc → packages-nexusm/apps/NexusMNfc (and .../base/packages/<X> →
// .../base/packages/NexusM<X>). Idempotent (skips an already-prefixed segment); leaves paths
// without an apps//base/packages/ marker untouched (so frameworks-nexusm/base/api is unchanged).
func applyRenamePrefix(path, prefix string) string {
	if prefix == "" {
		return path
	}
	for _, m := range []string{"/apps/", "/base/packages/"} {
		i := strings.Index(path, m)
		if i < 0 {
			continue
		}
		rest := path[i+len(m):]
		seg, tail := rest, ""
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			seg, tail = rest[:j], rest[j:]
		}
		if seg == "" || strings.HasPrefix(seg, prefix) {
			return path
		}
		return path[:i+len(m)] + prefix + seg + tail
	}
	return path
}

func requalifyLabel(s, outRoot string, laneMap map[string]string, cache map[string]bool, force bool, prefix string) string {
	if !strings.HasPrefix(s, "//") {
		return s
	}
	return requalifyPath(s[2:], "//", outRoot, laneMap, cache, force, prefix, s)
}

// requalifyBarePath rewrites an UNQUALIFIED root-relative path (include_dirs, aidl.include_dirs,
// cmd fragments) that points into another lane's tree. These are invisible to requalifyLabel
// because they carry no "//" — and they are the more dangerous form: a bare path resolves against
// the OTHER LANE'S FILES ON DISK, which are still present (the finder merely stops scanning their
// Android.bp). A cloned lane left with `include_dirs: ["frameworks-holo/rs"]` therefore compiles
// against the source lane's headers and looks green while silently not being sovereign.
func requalifyBarePath(s, outRoot string, laneMap map[string]string, cache map[string]bool, force bool, prefix string) string {
	if strings.HasPrefix(s, "//") || s == "" {
		return s
	}
	return requalifyPath(s, "", outRoot, laneMap, cache, force, prefix, s)
}

// requalifyEmbedded remaps a lane root that appears INSIDE a larger string rather than at its
// start — `$(location //frameworks-holo/base/core/java:protolog-groups)` in a genrule cmd,
// `--header-filter=^.*frameworks-holo/...`, `-Aroom.schemaLocation=packages-holo/...`,
// `-Wl,-exported_symbols_list,frameworks-holo/...`. The prefix forms above cannot see these, yet
// the $(location) case is BUILD-BLOCKING: it names a module in a lane this lunch drops, so soong
// fails to resolve it. Matching requires the trailing "/" so a root is replaced only as a whole
// path segment (frameworks-holo/ never matches frameworks-holo2/).
//
// This substitutes inside an already-PARSED string literal's value, not across file bytes — the
// AST still decides what is a string and what is structure.
func requalifyEmbedded(s string, laneMap map[string]string) string {
	for srcRoot, dstRoot := range laneMap {
		if srcRoot == "" {
			continue
		}
		// ⭐ ONLY a LANE-SCOPED token may be replaced mid-string. This function silently relied
		// on being called with a lane→lane map, where the token ("frameworks-holo/") cannot occur
		// anywhere but at a path root. A STOCK root has no such property: "packages/" appears as
		// an INTERIOR segment throughout the tree, so an unguarded ReplaceAll turns
		//     //frameworks/base/packages/SystemUI/aconfig:flags
		// into
		//     //frameworks-<lane>/base/packages-<lane>/SystemUI/aconfig:flags
		// — corrupting a path it was only supposed to re-root. Caught by TestRequalify the moment
		// the stock pass enabled bare paths (holo2test, 2026-08-20): the banked substring trap,
		// firing on the surgeon's own matcher. Guarded here rather than at the call site so no
		// future caller can reintroduce it.
		if !strings.Contains(srcRoot, "-") {
			continue
		}
		s = strings.ReplaceAll(s, srcRoot+"/", dstRoot+"/")
	}
	return s
}

// requalifyEmbeddedLabels repoints a QUALIFIED label that appears inside a larger string:
// `$(location //packages/services/x/utils/gen:gen)` in a genrule cmd. requalifyEmbedded cannot touch
// it (a STOCK root is never replaced mid-string, for the interior-segment reason documented there),
// and requalifyLabel never sees it (the string does not START with "//"). Yet in a whole-root fork
// the lane parallel of that label's directory exists, so the finder drops the stock bp and the label
// names nothing: "cmd: unknown location label" at Soong analysis (android-17 Holo landing, 2026-09-05,
// packages-holo/services/display_safety/service/harry-prebuilt).
//
// The "$(location" / "$(locations" prefix plus the "//" anchor identify a LABEL START, never an
// interior segment, so the token is handed to requalifyLabel exactly as a top-level label would be —
// same lane-parallel existence guard, same prefix handling. Unconditional (not gated on -paths):
// whenever the parallel exists the stock bp is dropped and the rewrite is mandatory.
var embeddedLocationLabel = regexp.MustCompile(`(\$\(locations?\s+)(//[^\s):]+(?::[^\s)]+)?)`)

func requalifyEmbeddedLabels(s, outRoot string, laneMap map[string]string, cache map[string]bool, force bool, prefix string) string {
	if !strings.Contains(s, "$(location") {
		return s
	}
	return embeddedLocationLabel.ReplaceAllStringFunc(s, func(m string) string {
		sub := embeddedLocationLabel.FindStringSubmatch(m)
		return sub[1] + requalifyLabel(sub[2], outRoot, laneMap, cache, force, prefix)
	})
}

// requalifyPath is the shared root-remap used by both the qualified and bare forms. body is the
// path with any "//" already stripped; lead is re-prepended on a hit. orig is returned unchanged
// when nothing matches, preserving the caller's exact string.
func requalifyPath(body, lead, outRoot string, laneMap map[string]string, cache map[string]bool, force bool, prefix string, orig string) string {
	pathPart, modPart := body, ""
	if i := strings.IndexByte(body, ':'); i >= 0 {
		pathPart, modPart = body[:i], body[i:]
	}
	for stockRoot, laneRoot := range laneMap {
		if pathPart != stockRoot && !strings.HasPrefix(pathPart, stockRoot+"/") {
			continue
		}
		lanePath := applyRenamePrefix(laneRoot+strings.TrimPrefix(pathPart, stockRoot), prefix)
		if force {
			return lead + lanePath + modPart // absolute-sovereignty: retarget even if not yet forked
		}
		exists, ok := cache[lanePath]
		if !ok {
			fi, err := os.Stat(filepath.Join(outRoot, lanePath))
			exists = err == nil && fi.IsDir()
			cache[lanePath] = exists
		}
		if exists {
			// ⭐ NAMESPACE GUARD — directory existence is NOT routing (holo2test, 2026-08-20).
			// The finder DROPS a lane bp that declares a soong_namespace and KEEPS the stock
			// parallel; that is deliberate, and the lane-root Android.bp template says so in its
			// own comment ("modules collapse to the global namespace … while keeping the stock
			// parallel for shared consumers"). So a //<lane-path>:<mod> label names a bp that is
			// never loaded at that path. requalify was fork-boundary aware but not namespace-aware
			// and repointed
			//   //frameworks/libs/native_bridge_support/android_api/libc:native_bridge_proxy_libc_defaults
			// onto the lane, after which soong called the module "undefined" while the .bp
			// defining it sat right there on disk at line 151.
			//
			// Only the QUALIFIED form is gated, and the distinction is the point: a `//path:mod`
			// label is resolved by SOONG against the loaded module graph, so it is bound by which
			// bp the finder actually loaded. A BARE path is resolved by the COMPILER against files
			// on disk, which are present regardless of routing — so relocating a bare path stays
			// correct even into a namespace-declaring dir. Same string, two different resolvers.
			// MIRRORS THE FINDER EXACTLY: apply<Lane>BpRoutes drops a namespace-declaring lane bp
			// UNLESS is<Lane>OwnedNamespaceBp(bp) — which is `strings.Contains(bp, "/pods/")`.
			// Decomposition pods stay namespaced AND stay loaded, so a label into a pod must name
			// the LANE; a label into any other namespace dir must stay STOCK. Verified against the
			// live routing receipt on holo2test: 18 namespace bps, 5 pods LOADED, 13 dropped.
			// Getting this wrong in either direction is a build error — too broad reverts the pods
			// (5 "depends on undefined module" at soong), too narrow leaves the native_bridge_support
			// label pointing at a bp the finder never loads.
			if modPart != "" && !strings.Contains("/"+lanePath, "/pods/") &&
				laneDirDeclaresNamespace(outRoot, lanePath, cache) {
				return orig
			}
			return lead + lanePath + modPart
		}
		return orig // target not forked — keep the original
	}
	return orig
}

// laneDirDeclaresNamespace is the DIRECTORY-keyed, memoised form of reexport.go's
// laneBpDeclaresNamespace. Cached under an "ns:" key so the hot path stays O(1); the prefix keeps
// it from colliding with the directory-existence entries sharing this map.
//
// ⭐ Note what this means: reexport.go already encoded the rule — its doc says the finder
// "collapses such a lane bp to root and never drops a stock parallel against it, so the re-export
// must not treat it as dropping stock either." The invariant was known and honoured in ONE
// subsystem; requalify simply never consulted it. This is a sibling applying a rule its neighbour
// already had, not a new discovery.
func laneDirDeclaresNamespace(outRoot, lanePath string, cache map[string]bool) bool {
	key := "ns:" + lanePath
	if v, ok := cache[key]; ok {
		return v
	}
	v := laneBpDeclaresNamespace(filepath.Join(outRoot, lanePath, "Android.bp"))
	cache[key] = v
	return v
}

// requalifyFile rewrites all qualified labels in one bp; returns whether it changed. When
// barePaths is set it ALSO rewrites unqualified root-relative paths (see requalifyBarePath).
func requalifyFile(path, outRoot string, laneMap map[string]string, cache map[string]bool, force bool, prefix string, barePaths bool) (bool, error) {
	return requalifyFileCfg(path, outRoot, laneMap, cache, force, prefix, barePaths, nil)
}

// soongConfigRenamer returns a function rewriting soong_config namespace/variable identifiers from
// srcLane to newLane ("holo_framework_routing" → "holotest_framework_routing",
// "enable_holo_res" → "enable_holotest_res"). Matching is on whole underscore-delimited tokens, so
// a lane name never matches a substring of an unrelated identifier.
func soongConfigRenamer(srcLane, newLane string) func(string) string {
	if srcLane == "" || newLane == "" || srcLane == newLane {
		return nil
	}
	return func(v string) string {
		parts := strings.Split(v, "_")
		hit := false
		for i, p := range parts {
			if p == srcLane {
				parts[i] = newLane
				hit = true
			}
		}
		if !hit {
			return v
		}
		return strings.Join(parts, "_")
	}
}

// requalifyFileCfg is requalifyFile plus an optional soong_config identifier renamer.
func requalifyFileCfg(path, outRoot string, laneMap map[string]string, cache map[string]bool, force bool, prefix string, barePaths bool, soongCfg func(string) string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	file, errs := parser.Parse(path, bytes.NewReader(b))
	if len(errs) > 0 {
		return false, errs[0]
	}
	changed := false
	var walk func(e parser.Expression)
	walk = func(e parser.Expression) {
		switch v := e.(type) {
		case *parser.String:
			nv := requalifyLabel(v.Value, outRoot, laneMap, cache, force, prefix)
			if nv == v.Value && barePaths {
				nv = requalifyBarePath(v.Value, outRoot, laneMap, cache, force, prefix)
			}
			if nv == v.Value && barePaths {
				nv = requalifyEmbedded(v.Value, laneMap)
			}
			if nv == v.Value {
				nv = requalifyEmbeddedLabels(v.Value, outRoot, laneMap, cache, force, prefix)
			}
			if nv != v.Value {
				v.Value = nv
				changed = true
			}
		case *parser.List:
			for _, it := range v.Values {
				walk(it)
			}
		case *parser.Map:
			for _, p := range v.Properties {
				walk(p.Value)
			}
		case *parser.Operator:
			walk(v.Args[0])
			walk(v.Args[1])
		case *parser.Select:
			// select() bodies hold real labels (a lane-aware filegroup routes its srcs through
			// one). Without this case the whole subtree is invisible to the rewrite and the
			// lane keeps a cross-lane reference that no stale-form grep of the property finds.
			for _, c := range v.Cases {
				if c != nil && c.Value != nil {
					walk(c.Value)
				}
			}
			// The select CONDITION names a soong_config namespace + variable, which for a
			// lane-sourced clone still names the SOURCE lane. That is not a path, so no path
			// rewrite can reach it — and a condition naming a namespace this lunch never sets
			// simply takes the default branch. The lane then silently builds the STOCK side of
			// its own lane-aware select, with no error anywhere.
			if soongCfg != nil {
				for ci := range v.Conditions {
					for ai := range v.Conditions[ci].Args {
						a := &v.Conditions[ci].Args[ai]
						if nv := soongCfg(a.Value); nv != a.Value {
							a.Value = nv
							changed = true
						}
					}
				}
			}
		}
	}
	for _, def := range file.Defs {
		switch d := def.(type) {
		case *parser.Module:
			for _, p := range d.Properties {
				walk(p.Value)
			}
		case *parser.Assignment:
			walk(d.Value)
		}
	}
	if !changed {
		return false, nil
	}
	out, err := parser.Print(file)
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, out, 0o644)
}

// runRequalify walks the lane's cloned frameworks-<lane>/ + packages-<lane>/ trees and repoints
// every internal qualified label whose target was forked. Skips files that fail to parse (logs).
func runRequalify(c LaneConfig, outRoot string) (changed, failed int) {
	laneMap := map[string]string{
		"frameworks": "frameworks-" + c.Name,
		"packages":   "packages-" + c.Name,
	}
	cache := map[string]bool{}
	roots := []string{"frameworks-" + c.Name, "packages-" + c.Name}
	fmt.Printf("\nrequalify (repoint //<root>/… → //<root>-%s/… for forked targets, AST-safe):\n", c.Name)
	for _, root := range roots {
		rootDir := filepath.Join(outRoot, root)
		if fi, err := os.Stat(rootDir); err != nil || !fi.IsDir() {
			continue
		}
		filepath.Walk(rootDir, func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
				return nil
			}
			// barePaths=false here is DELIBERATE and correct for the configuration this pass was
			// designed for — a SUBTREE fork. Contrast runRequalifyFromLane, which passes true.
			// The asymmetry follows from what a stale bare path MEANS in each direction:
			//
			//   lane-sourced clone : `include_dirs: ["frameworks-<src>/rs"]` resolves against the
			//                        SOURCE LANE's files, still on disk (the finder only stops
			//                        scanning their Android.bp). It compiles green against the wrong
			//                        tree — silently non-sovereign. See requalifyBarePath's doc.
			//                        Mandatory, hence true.
			//   stock-sourced fork : `include_dirs: ["frameworks/base/core/java"]` resolves against
			//                        STOCK, which is the CORRECT fallback for anything the lane did
			//                        not fork. For a subtree fork that is most of them, so a blanket
			//                        rewrite would be mostly no-ops. Hence false.
			//
			// ⚠️ A WHOLE-ROOT fork (-fork frameworks,packages) inverts that ratio: nearly every bare
			// stock path now HAS a lane parallel, and each one is a dual supplier — two roots feeding
			// the same header or aidl type. Measured on holo2test (2026-08-20, 178k files): 584 such
			// refs across 137 bp, where the mature keep-name reference lane frameworks-holo carries 3.
			// Symptoms are remote from the cause — `Duplicate files found for …IGeofenceHardware`, and
			// `redefinition of FloatRect` where a #pragma once header exists under two paths.
			//
			// ⇒ This is a CONFIGURATION the pass predates, not a defect in it. requalifyPath already
			// gates every rewrite on the lane parallel EXISTING, so `requalify -paths` is safe to run
			// for a whole-root fork and is the documented instrument for it (README: "with -paths also
			// BARE root-relative paths (include_dirs) + paths embedded in genrule cmd strings").
			// It stays off HERE so a subtree fork keeps its conservative default.
			//
			// ⛔ HISTORICAL NOTE, corrected: an earlier revision of this comment claimed tree-wide
			// -paths "changes the dependency graph far beyond the modules that needed it", citing a
			// blueprint panic (parallelVisit ran 142724, expected 142736). That was a WRONG diagnosis
			// inferred from a symptom. The panic came from requalifyEmbedded being reached with a
			// STOCK laneMap — a map it was never designed for — whose mid-string ReplaceAll corrupted
			// interior segments (//frameworks/base/packages/SystemUI → …/base/packages-<lane>/SystemUI),
			// making those modules unreachable. With the guard in requalifyEmbedded, the same tree-wide
			// run repointed 135 bp with ZERO corrupted paths and NO panic. Breadth was never the issue.
			ch, ferr := requalifyFile(p, outRoot, laneMap, cache, false, "", false)
			if ferr != nil {
				failed++
				return nil // resilient: skip unparseable/odd bp, keep going
			}
			if ch {
				changed++
			}
			return nil
		})
	}
	fmt.Printf("  %d bp repointed, %d skipped (parse).\n", changed, failed)
	return changed, failed
}

// runRequalifyFromLane repoints a LANE-SOURCED clone from its source lane onto this lane —
// qualified labels, bare root-relative paths, and paths embedded in larger strings. Used by
// `create -from <lane>`; the stock-direction pass (runRequalify) runs first and is unaffected.
func runRequalifyFromLane(c LaneConfig, outRoot, srcLane string) (changed, failed int) {
	soongCfg := soongConfigRenamer(srcLane, c.Name)
	laneMap := map[string]string{
		"frameworks-" + srcLane: "frameworks-" + c.Name,
		"packages-" + srcLane:   "packages-" + c.Name,
	}
	cache := map[string]bool{}
	fmt.Printf("\nrequalify (lane-sourced) //<root>-%s/… → //<root>-%s/…  + bare/embedded paths:\n", srcLane, c.Name)
	for _, root := range []string{"frameworks-" + c.Name, "packages-" + c.Name} {
		rootDir := filepath.Join(outRoot, root)
		if fi, err := os.Stat(rootDir); err != nil || !fi.IsDir() {
			continue
		}
		filepath.Walk(rootDir, func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
				return nil
			}
			ch, ferr := requalifyFileCfg(p, outRoot, laneMap, cache, false, "", true, soongCfg)
			if ferr != nil {
				failed++
				return nil
			}
			if ch {
				changed++
			}
			return nil
		})
	}
	fmt.Printf("  %d bp repointed, %d skipped (parse).\n", changed, failed)
	return changed, failed
}

// laneSourcePathExts are the NON-blueprint file types that carry lane paths the build actually
// consumes. A verbatim lane clone keeps its source lane's paths inside these files, and unlike a
// stale Android.bp label the failure is remote from the edit: a .proto's
// `import "frameworks-<src>/…/Resources.proto"` is compiled into a generated .pb.h whose #include
// still names the source lane, so the error surfaces in a generated header nobody wrote.
//
// Split in two tiers only to keep the report honest about what was touched: tier 1 breaks the
// build, tier 2 is references in source/doc text. Both are rewritten; anything else is reported,
// never silently edited (notably .json, where absolute paths are usually provenance records of the
// source lane's files rather than build inputs).
var laneSourcePathExts = []string{
	// tier 1 — build-consumed
	".proto", ".aidl", ".cpp", ".cc", ".cxx", ".c", ".h", ".hpp", ".inc", ".mk", ".py", ".sh", ".rs",
	// .go: a lane can own soong build logic (bootstrap_go_package), e.g. base/api/api.go setting
	// props.Visibility to the SOURCE lane — a grant to a lane this lunch never loads.
	".go",
	// tier 2 — source/doc references
	".java", ".kt", ".xml", ".md",
}

// relocateLaneSourcePaths rewrites `<root>-<src>/` → `<root>-<new>/` inside one file's bytes.
// Whole-segment by construction: the trailing "/" is required, so frameworks-holo/ can never
// match frameworks-holo2/. There is no AST for these formats — a path token replace is the
// available instrument, and it is safe precisely because the token is a lane root plus separator
// rather than a structural pattern.
func relocateLaneSourcePaths(b []byte, srcLane, newLane string) ([]byte, bool) {
	changed := false
	for _, root := range []string{"frameworks", "packages"} {
		from := []byte(root + "-" + srcLane + "/")
		to := []byte(root + "-" + newLane + "/")
		if bytes.Contains(b, from) {
			b = bytes.ReplaceAll(b, from, to)
			changed = true
		}
	}
	return b, changed
}

// runRelocateLaneSourcePaths applies the above across the lane trees and reports what it changed,
// plus what it deliberately left alone. Used by `create -from`.
func runRelocateLaneSourcePaths(c LaneConfig, outRoot, srcLane string) (changed int) {
	want := map[string]bool{}
	for _, e := range laneSourcePathExts {
		want[e] = true
	}
	byExt := map[string]int{}
	skipped := map[string]int{}
	fmt.Printf("\nrelocate lane paths in NON-bp sources (%s → %s):\n", srcLane, c.Name)
	for _, root := range []string{"frameworks-" + c.Name, "packages-" + c.Name} {
		rootDir := filepath.Join(outRoot, root)
		if fi, err := os.Stat(rootDir); err != nil || !fi.IsDir() {
			continue
		}
		filepath.Walk(rootDir, func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || fi.Size() > 8<<20 {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			nb, ch := relocateLaneSourcePaths(b, srcLane, c.Name)
			if !ch {
				return nil
			}
			if !want[ext] {
				skipped[ext]++ // references the source lane but is not a type we rewrite
				return nil
			}
			if os.WriteFile(p, nb, fi.Mode().Perm()) == nil {
				byExt[ext]++
				changed++
			}
			return nil
		})
	}
	for _, e := range laneSourcePathExts {
		if byExt[e] > 0 {
			fmt.Printf("  %-7s %d file(s)\n", e, byExt[e])
		}
	}
	if len(skipped) > 0 {
		fmt.Printf("  left alone (still reference %s, not a build-consumed type):", srcLane)
		for e, n := range skipped {
			fmt.Printf(" %s×%d", e, n)
		}
		fmt.Println()
	}
	fmt.Printf("  %d file(s) rewritten.\n", changed)
	return changed
}

// ─────────────────────────────────────────────────────────────────────────────────────────────
// STOCK-SOURCED relocation (`create -fork <stock subtrees>` with no -from)
//
// PROVEN ON: holo2test, 2026-08-20 — the first lane seeded from STOCK frameworks/ + packages/
// (178,264 files, whole-root fork) driven to a green `m droid` emulator image.
//
// WHY THE STOCK DIRECTION NEEDS ITS OWN PASS. A lane→lane relocate (relocateLaneSourcePaths)
// replaces UNCONDITIONALLY, because its token `frameworks-<src>/` is lane-scoped: it cannot occur
// inside a reference that must stay put. The stock token `frameworks/` has no such property — it
// appears in EVERY reference, including ones that MUST keep pointing at stock (a subtree the lane
// deliberately did not fork). So every rewrite here is gated on the lane parallel EXISTING.
//
// ⭐ AND THE GUARD MUST TEST THE RIGHT ARTIFACT. For a GENERATED header the file never exists in
// the source tree — it only appears in the out dir after its generator runs — so an on-disk test
// against the header always fails and silently declines a rewrite that was required. The guard has
// to test the PRODUCER instead. Measured cost of getting this wrong: 37 `.pb.h` + 30 `.proto.h`
// includes silently skipped, surfacing thousands of ninja steps later as `file not found` inside
// files nobody wrote.
// ─────────────────────────────────────────────────────────────────────────────────────────────

// stockRelocSpec is one relocatable reference form: how to find it, and which artifact proves the
// lane owns the target. Keeping the producer mapping beside the pattern is what stops a future
// reader re-deriving "why does a .pb.h test a .proto?".
type stockRelocSpec struct {
	name string
	exts []string       // file types to scan
	re   *regexp.Regexp // groups: 1=lead 2=root 3=path 4=trail
	// producer maps a referenced path to the artifact whose existence proves lane ownership.
	// Identity for real files; for generated headers it maps back to the source that emits them.
	producer func(string) string
}

func identityProducer(rel string) string { return rel }

// generatedHeaderProducer maps a generated C/C++ header back to the .proto that emits it.
// Two generators are in play in AOSP and they use DIFFERENT suffixes:
//
//	protobuf C++   foo.proto  ->  foo.pb.h
//	streaming_proto foo.proto ->  foo.proto.h
func generatedHeaderProducer(rel string) string {
	switch {
	case strings.HasSuffix(rel, ".pb.h"):
		return strings.TrimSuffix(rel, ".pb.h") + ".proto"
	case strings.HasSuffix(rel, ".proto.h"):
		return strings.TrimSuffix(rel, ".h")
	}
	return rel
}

// stockRelocSpecs is the full set. Each entry earned its place from a real build failure.
var stockRelocSpecs = []stockRelocSpec{
	{
		// `import "frameworks/base/core/proto/x.proto";` inside a lane .proto. protoc resolves the
		// import against STOCK and bakes a stock-shaped #include into the generated .pb.h, which the
		// lane-rooted -I cannot satisfy. NOTE BOTH ROOTS: an earlier revision matched only
		// `frameworks/` and silently left 17 `packages/` imports across 13 files — a tree with two
		// lane roots reporting hits from only one is an incomplete scan, not a clean one.
		name:     "proto import",
		exts:     []string{".proto"},
		re:       regexp.MustCompile(`(?m)(\s*import\s+")(frameworks|packages)/([^"]+)(";)`),
		producer: identityProducer,
	},
	{
		// `#include "frameworks/base/core/proto/x.pb.h"` in hand-written C/C++. The header is
		// GENERATED, so it is never on disk — test the .proto that produces it.
		name:     "generated-header include",
		exts:     []string{".cpp", ".cc", ".cxx", ".c", ".h", ".hpp"},
		re:       regexp.MustCompile(`(#include\s+[<"])(frameworks|packages)/([^">]+)([">])`),
		producer: generatedHeaderProducer,
	},
}

// runRelocateStockSourcePaths applies every spec across the lane trees. Used by `create` WITHOUT
// -from, and by `requalify -sources` without -from. Blueprint files are NOT touched here — they go
// through requalify (labels) and `requalify -paths` (bare + embedded paths), which are AST-safe.
func runRelocateStockSourcePaths(c LaneConfig, outRoot string) (changed, moved, kept int) {
	laneRoot := map[string]string{"frameworks": "frameworks-" + c.Name, "packages": "packages-" + c.Name}
	exists := map[string]bool{}
	has := func(rel string) bool {
		if v, ok := exists[rel]; ok {
			return v
		}
		_, err := os.Stat(filepath.Join(outRoot, rel))
		exists[rel] = err == nil
		return err == nil
	}
	fmt.Printf("\nrelocate STOCK paths in NON-bp sources (existence-guarded, producer-aware):\n")
	for _, spec := range stockRelocSpecs {
		want := map[string]bool{}
		for _, e := range spec.exts {
			want[e] = true
		}
		specMoved, specFiles := 0, 0
		for _, root := range []string{"frameworks-" + c.Name, "packages-" + c.Name} {
			rootDir := filepath.Join(outRoot, root)
			if fi, err := os.Stat(rootDir); err != nil || !fi.IsDir() {
				continue
			}
			filepath.Walk(rootDir, func(p string, fi os.FileInfo, e error) error {
				if e != nil || fi.IsDir() || fi.Size() > 8<<20 || !want[strings.ToLower(filepath.Ext(p))] {
					return nil
				}
				b, rerr := os.ReadFile(p)
				if rerr != nil {
					return nil
				}
				n := 0
				out := spec.re.ReplaceAllFunc(b, func(m []byte) []byte {
					g := spec.re.FindSubmatch(m)
					lane := laneRoot[string(g[2])] + "/" + string(g[3])
					if !has(laneRoot[string(g[2])] + "/" + spec.producer(string(g[3]))) {
						kept++
						return m // no lane parallel — this reference MUST stay stock
					}
					n++
					return []byte(string(g[1]) + lane + string(g[4]))
				})
				if n > 0 && os.WriteFile(p, out, fi.Mode().Perm()) == nil {
					specMoved += n
					specFiles++
					changed++
				}
				return nil
			})
		}
		moved += specMoved
		fmt.Printf("  %-26s %4d reference(s) in %d file(s)\n", spec.name, specMoved, specFiles)
	}
	fmt.Printf("  %d left stock (no lane parallel — correct, NOT a miss).\n", kept)
	return changed, moved, kept
}
