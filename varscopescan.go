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
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/internal/blueprint/parser"
)

// blueprintReservedDirectives are the assignment-shaped names Blueprint marks NON-inheritable
// (build/blueprint/context.go: scope.DontInherit("subdirs"/"optional_subdirs"/"build")). They are
// subfile/subdir INCLUSION directives, not scoped variables: `build = ["Foo.bp"]` names sibling bps
// to parse into THIS scope, and none of the three flows into a subdirectory bp. So an Android.bp whose
// only top-level assignment is one of these declares NO scoped variable — treating it as a var-definer
// is a false positive that makes the finder subtree-drop stock dirs that share no scope with it (e.g.
// frameworks/base/Android.bp carries only `build = [...]`, yet the drop nuked the whole stock
// frameworks/base subtree, including the deliberately-kept-stock api). Skip them.
var blueprintReservedDirectives = map[string]bool{
	"build": true, "subdirs": true, "optional_subdirs": true,
}

// bpDefinesTopLevelVarAST reports — via the Blueprint AST, not a line scan — whether an Android.bp
// declares any top-level VARIABLE (an Assignment at file scope, e.g. `min_launcher3_sdk_version = "31"`).
// A top-level variable is scoped to its file AND every bp it INCLUDES, so a lane finder that drops such a
// bp must drop the co-scoped stock bps, or a kept stock referencer is orphaned ("undefined variable" at
// bootstrap). The reserved subfile/subdir directives (build/subdirs/optional_subdirs) are NOT variables
// and are excluded. This is the seed-time authority the finder's runtime cannot compute.
func bpDefinesTopLevelVarAST(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	file, _ := parser.Parse(path, bytes.NewReader(b))
	if file == nil {
		return false
	}
	for _, def := range file.Defs {
		if a, ok := def.(*parser.Assignment); ok && !blueprintReservedDirectives[a.Name] {
			return true
		}
	}
	return false
}

// stockVarDefinerBps AST-scans the lane's forked trees under outRoot (frameworks-<lane>/, packages-<lane>/)
// and returns the STOCK bp paths whose verbatim-forked content declares a top-level variable — the stock
// variable-scope roots. The lane fork copies the definition verbatim, so a lane bp defines the variable iff
// its stock parallel does; recording the stock path lets the generated finder drop the whole stock subtree
// under any of these it drops. Best-effort: unreadable/unparseable bps are skipped, never guessed.
func stockVarDefinerBps(lane, outRoot string) []string {
	if outRoot == "" {
		return nil
	}
	seen := map[string]bool{}
	for _, r := range []struct{ laneDir, stockDir string }{
		{"frameworks-" + lane, "frameworks"},
		{"packages-" + lane, "packages"},
	} {
		root := filepath.Join(outRoot, r.laneDir)
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Name() != "Android.bp" {
				return nil
			}
			if !bpDefinesTopLevelVarAST(p) {
				return nil
			}
			rel, relErr := filepath.Rel(root, p)
			if relErr != nil {
				return nil
			}
			seen[r.stockDir+"/"+filepath.ToSlash(rel)] = true
			return nil
		})
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// unforkedRefIndex collects every module name referenced from an UN-FORKED path — every top-level dir
// under outRoot except the fork roots (frameworks*, packages*), build outputs (out*), and hidden/_ dirs.
// These are the consumers a whole-root frameworks+packages fork does NOT reroute, so a stock module
// referenced here must stay defined under its stock name. Grammar-complete via depNamesInBp (walks
// select/operator and nested conditional containers a text grep would miss).
func unforkedRefIndex(outRoot string) map[string]bool {
	refs := map[string]bool{}
	entries, err := os.ReadDir(outRoot)
	if err != nil {
		return refs
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, "out") || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_") ||
			strings.HasPrefix(n, "frameworks") || strings.HasPrefix(n, "packages") {
			continue
		}
		_ = filepath.WalkDir(filepath.Join(outRoot, n), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Name() != "Android.bp" {
				return nil
			}
			depNamesInBp(p, refs)
			return nil
		})
	}
	return refs
}

// bpAllLaneNamed reports whether every module a bp declares is prefixed with the lane's CamelCase token
// — the additive-keep test (mirrors the finder's <lane>ForkKeepsStock), computed via the AST. No modules
// → not additive (an infra/prebuilt/config bp is a replacement, handled by the finder's normal drop).
func bpAllLaneNamed(path, camel string) bool {
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

// stockInForkOnlyBps returns the STOCK bp paths the finder's additive-keep would KEEP but that are
// in-fork DEAD WEIGHT: every module their all-Camel lane parallel additively shadows is referenced ONLY
// from inside the fork, where consumers already resolve the lane's Camel names. Keeping such a stock
// parallel is redundant and dangles it on any dropped-replacement dependency (the _defaults class). The
// finder drops these on top of its normal replacement drops. A stock module referenced from ANY un-forked
// path is never listed — so nothing an out-of-fork consumer needs is dropped. This is the reference-test
// that generalizes the fragile per-module var-guard: keep a stock parallel iff it is reachable off-fork.
func stockInForkOnlyBps(lane, camel, outRoot string) []string {
	if outRoot == "" || camel == "" {
		return nil
	}
	unforked := unforkedRefIndex(outRoot)
	seen := map[string]bool{}
	var drop []string
	for _, r := range []struct{ laneDir, stockDir string }{
		{"frameworks-" + lane, "frameworks"},
		{"packages-" + lane, "packages"},
	} {
		root := filepath.Join(outRoot, r.laneDir)
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Name() != "Android.bp" {
				return nil
			}
			if !bpAllLaneNamed(p, camel) {
				return nil // not additive → a replacement; its stock parallel already drops
			}
			rel, relErr := filepath.Rel(root, p)
			if relErr != nil {
				return nil
			}
			stockNames, _ := bpModules(filepath.Join(outRoot, r.stockDir, rel))
			if len(stockNames) == 0 {
				return nil // no stock parallel (or empty) → nothing to drop
			}
			for m := range stockNames {
				if unforked[m] {
					return nil // referenced off-fork → KEEP the stock parallel
				}
			}
			stockRel := r.stockDir + "/" + filepath.ToSlash(rel)
			if !seen[stockRel] {
				seen[stockRel] = true
				drop = append(drop, stockRel)
			}
			return nil
		})
	}
	sort.Strings(drop)
	return drop
}

// offForkReferenced reports whether a stock module name, or any of its Soong-derived forms (X-cpp,
// X.stubs.module_lib, X-aconfig-java, …), appears in the off-fork reference index — i.e. some un-forked
// consumer depends on it. Uses the census's derivedSuffixes so a ref to X-cpp counts as needing base X.
func offForkReferenced(name string, offFork map[string]bool) bool {
	if offFork[name] {
		return true
	}
	for _, suf := range derivedSuffixes {
		if offFork[name+suf] {
			return true
		}
	}
	return false
}

// laneModuleSet returns every module name the lane declares across its forked frameworks/packages trees.
func laneModuleSet(outRoot, lane string) map[string]bool {
	set := map[string]bool{}
	for _, root := range []string{"frameworks-" + lane, "packages-" + lane} {
		_ = filepath.WalkDir(filepath.Join(outRoot, root), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Name() != "Android.bp" {
				return nil
			}
			names, _ := bpModules(p)
			for n := range names {
				set[n] = true
			}
			return nil
		})
	}
	return set
}
