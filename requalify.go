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

	parser "github.com/abstractsrevenge/sovereign_lane_surgeon/internal/blueprint/parser"
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
		s = strings.ReplaceAll(s, srcRoot+"/", dstRoot+"/")
	}
	return s
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
			return lead + lanePath + modPart
		}
		return orig // target not forked — keep the original
	}
	return orig
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
