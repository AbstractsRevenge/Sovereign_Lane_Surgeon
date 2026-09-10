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
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/internal/blueprint/parser"
)

// keepnamepass.go — the `keepname-offork` subcommand: keep off-fork-referenced modules KEEP-NAME.
//
// A rename-model lane renames a module X to <Camel>X and repoints its in-fork consumers. But a module
// referenced from an UN-FORKED path (external/system/device/hardware/build) is NOT repointed — the fork
// doesn't reach those consumers — so they still say X, the lane's renamed <Camel>X doesn't answer, and
// X's replacement definer is dropped: "depends on undefined module X". The finder keep-vs-drop
// (stock_infork_only_bps) is the mirror of this on the drop side; this is the rename side.
//
// The pass, driven by the same off-fork reference index: for every module the lane defines RENAMED
// (<Camel>X) whose base X is referenced off-fork and which the lane does NOT already define keep-name,
// un-rename <Camel>X -> X across the lane (def + all refs, derived-suffix-aware via renameAndRepointBp).
// Then off-fork consumers resolve X against the lane's keep-name module and the stock replacement can drop.
// Dry-run by default; -apply writes. Both lanes (holo2, nexusm) run this one implementation.
func cmdKeepNameOffork(args []string) int {
	fs := flag.NewFlagSet("keepname-offork", flag.ExitOnError)
	name := fs.String("name", "", "lane name (e.g. holo2)")
	out := fs.String("out", "", "AOSP root")
	only := fs.String("only", "", "comma-separated stock base names to un-rename UNCONDITIONALLY (targeted keep-name for a known off-fork ref, bypassing the broad off-fork detection)")
	apply := fs.Bool("apply", false, "write the un-renames (default: dry-run — print the map)")
	_ = fs.Parse(args)
	if *name == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "keepname-offork: -name and -out are required")
		return 2
	}
	camel := *name
	if *name != "" {
		camel = strings.ToUpper((*name)[:1]) + (*name)[1:]
	}

	laneModules := laneModuleSet(*out, *name)

	rename := map[string]string{}
	if *only != "" {
		// Targeted un-rename: exactly the named bases, if the lane defines Camel<base>. Used for a base
		// known to be genuinely off-fork-referenced (e.g. android.frameworks.stats, imported by system/*,
		// external/*, hardware/*) where the broad detector over- or under-flags.
		for _, b := range strings.Split(*only, ",") {
			b = strings.TrimSpace(b)
			if b == "" {
				continue
			}
			if laneModules[camel+b] {
				rename[camel+b] = b
			}
		}
	} else {
		offFork := unforkedRefIndex(*out)
		for r := range laneModules {
			if !strings.HasPrefix(r, camel) {
				continue
			}
			base := r[len(camel):]
			if base == "" || laneModules[base] {
				continue // empty, or the lane already declares the keep-name base (genuinely additive)
			}
			if offForkReferenced(base, offFork) {
				rename[r] = base
			}
		}
	}

	keys := make([]string, 0, len(rename))
	for k := range rename {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Printf("keepname-offork: %d off-fork-referenced module(s) the lane renamed -> un-rename to keep-name (lane %q)\n", len(rename), *name)
	for i, k := range keys {
		if i >= 20 {
			fmt.Printf("  ... and %d more\n", len(keys)-20)
			break
		}
		fmt.Printf("  %s -> %s\n", k, rename[k])
	}
	// keepTargets are the keep-name names that must look like stock to off-fork consumers: the un-rename
	// values, plus (for -only) the requested bases even when already keep-name — so a repeat run still
	// strips a stale owner.
	keepTargets := map[string]bool{}
	for _, v := range rename {
		keepTargets[v] = true
	}
	if *only != "" {
		for _, b := range strings.Split(*only, ",") {
			if b = strings.TrimSpace(b); b != "" {
				keepTargets[b] = true
			}
		}
	}
	if len(rename) == 0 && len(keepTargets) == 0 {
		return 0
	}
	if !*apply {
		fmt.Println("(dry-run; pass -apply to write)")
		return 0
	}
	changed, failed := applyRenameMapToLane(*out, *name, rename)
	// A module un-renamed to keep-name for an OFF-FORK consumer must look like stock to it. For an
	// aidl_interface that means UNOWNED: the rename pass set `owner: "<lane>"` (lane-owned interfaces get
	// an independent freeze schedule), but a stock importer cannot depend on an owned interface
	// ("imports X which is an interface owned by <lane> … not allowed"). Strip owner from the keep-name
	// targets so an off-fork import (system/*, hardware/*) resolves against an unowned interface, as stock.
	stripped := stripAidlOwner(*out, *name, keepTargets)
	// A module un-renamed to keep-name that previously carried `overrides: ["<stock>"]` (the Model-A
	// identity rename made Holo2<stock> override the dropped stock module) now NAMES itself <stock>, so
	// that entry is a self-override (module X overrides X). Strip the self-referential entry (and drop an
	// emptied overrides list) so it doesn't confuse Soong's override mutator.
	selfOv := stripSelfOverrides(*out, *name, keepTargets)
	fmt.Printf("keepname-offork: applied — %d bp(s) changed, %d owner-stripped, %d self-override-stripped, %d failed\n", changed, stripped, selfOv, failed)
	if failed > 0 {
		return 1
	}
	return 0
}

// stripAidlOwner removes `owner:` from every aidl_interface in the lane whose name is a keep-name target,
// so an off-fork consumer imports it as an unowned (stock-like) interface. Returns bp count changed.
func stripAidlOwner(outRoot, lane string, keepTargets map[string]bool) int {
	changed := 0
	for _, root := range []string{"frameworks-" + lane, "packages-" + lane} {
		_ = filepath.WalkDir(filepath.Join(outRoot, root), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !isBpFile(d.Name()) {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			file, errs := parser.Parse("", bytes.NewReader(b))
			if len(errs) > 0 || file == nil {
				return nil
			}
			bpChanged := false
			for _, def := range file.Defs {
				mod, ok := def.(*parser.Module)
				if !ok || mod.Type != "aidl_interface" {
					continue
				}
				var nm string
				for _, pr := range mod.Properties {
					if pr.Name == "name" {
						if s, ok := pr.Value.(*parser.String); ok {
							nm = s.Value
						}
					}
				}
				if keepTargets[nm] && removeProp(mod, "owner") {
					bpChanged = true
				}
			}
			if !bpChanged {
				return nil
			}
			outB, perr := parser.Print(file)
			if perr != nil {
				return nil
			}
			fi, _ := os.Stat(p)
			mode := os.FileMode(0o644)
			if fi != nil {
				mode = fi.Mode()
			}
			if os.WriteFile(p, outB, mode) == nil {
				changed++
			}
			return nil
		})
	}
	return changed
}

// stripSelfOverrides removes a module's own name from its `overrides:` list when that name is a keep-name
// target (a self-override left behind by un-renaming a Model-A identity module Holo2X → X, whose
// overrides:["X"] then points at itself). If the overrides list is emptied, the property is removed.
// Returns the number of bp files changed. Only the module's OWN name is stripped — real overrides of
// OTHER modules are preserved.
func stripSelfOverrides(outRoot, lane string, keepTargets map[string]bool) int {
	changed := 0
	roots := []string{"frameworks-" + lane, "packages-" + lane}
	if ents, err := os.ReadDir(filepath.Join(outRoot, "device", "google")); err == nil {
		for _, e := range ents {
			if e.IsDir() && strings.HasSuffix(e.Name(), "-"+lane) {
				roots = append(roots, filepath.Join("device", "google", e.Name()))
			}
		}
	}
	for _, root := range roots {
		_ = filepath.WalkDir(filepath.Join(outRoot, root), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !isBpFile(d.Name()) {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			file, errs := parser.Parse("", bytes.NewReader(b))
			if len(errs) > 0 || file == nil {
				return nil
			}
			bpChanged := false
			for _, def := range file.Defs {
				mod, ok := def.(*parser.Module)
				if !ok {
					continue
				}
				var nm string
				for _, pr := range mod.Properties {
					if pr.Name == "name" {
						if s, ok := pr.Value.(*parser.String); ok {
							nm = s.Value
						}
					}
				}
				if nm == "" || !keepTargets[nm] {
					continue
				}
				for _, pr := range mod.Properties {
					if pr.Name != "overrides" {
						continue
					}
					lst, ok := pr.Value.(*parser.List)
					if !ok {
						continue
					}
					kept := lst.Values[:0]
					for _, v := range lst.Values {
						if s, ok := v.(*parser.String); ok && s.Value == nm {
							bpChanged = true
							continue // drop the self-override entry
						}
						kept = append(kept, v)
					}
					lst.Values = kept
					if len(kept) == 0 {
						if removeProp(mod, "overrides") {
							bpChanged = true
						}
					}
				}
			}
			if !bpChanged {
				return nil
			}
			outB, perr := parser.Print(file)
			if perr != nil {
				return nil
			}
			fi, _ := os.Stat(p)
			mode := os.FileMode(0o644)
			if fi != nil {
				mode = fi.Mode()
			}
			if os.WriteFile(p, outB, mode) == nil {
				changed++
			}
			return nil
		})
	}
	return changed
}

// applyRenameMapToLane rewrites every Android.bp in the lane's forked trees + its device products with the
// rename map (un-rename defs + repoint all refs, derived-suffix-aware via renameAndRepointBp), writing the
// changed files. Mutation only reaches the lane's own dirs; off-fork and stock bps are never touched.
func applyRenameMapToLane(outRoot, lane string, rename map[string]string) (changed, failed int) {
	roots := []string{"frameworks-" + lane, "packages-" + lane}
	if ents, err := os.ReadDir(filepath.Join(outRoot, "device", "google")); err == nil {
		for _, e := range ents {
			if e.IsDir() && strings.HasSuffix(e.Name(), "-"+lane) {
				roots = append(roots, filepath.Join("device", "google", e.Name()))
			}
		}
	}
	for _, root := range roots {
		_ = filepath.Walk(filepath.Join(outRoot, root), func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || !isBpFile(filepath.Base(p)) {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			outB, ch, ferr := renameAndRepointBp(b, rename, lane)
			if ferr != nil {
				failed++
				return nil
			}
			if ch {
				if werr := os.WriteFile(p, outB, fi.Mode()); werr == nil {
					changed++
				} else {
					failed++
				}
			}
			return nil
		})
	}
	return changed, failed
}
