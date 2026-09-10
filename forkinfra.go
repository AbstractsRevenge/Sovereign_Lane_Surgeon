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

// forkinfra.go — the `fork-shared-infra` subcommand: fork a shared-infra subtree KEEP-NAME into a
// rename-model lane, repointing only its outgoing refs.
//
// The rename model deliberately leaves some frameworks/ subtrees stock (bpmirror defaultInfraExcludes:
// frameworks/base/api under KeepName=false) because they are the API SURFACE — renaming their modules
// (framework-non-updatable-unbundled-defaults, the droidstubs/sdk-annotations defaults, soong-api) would
// cascade into every apex and the api signature check. But leaving them STOCK is a regressive cross-tree
// dependency: a lane consumer reaching OUT to a stock variant for something that should live inside the
// lane. Both are wrong. The right shape is lane SELF-RELIANCE without straying from the API:
//
//   • FORK the subtree into the lane tree (own it in-repo), but KEEP-NAME — the module names are the
//     stock a17 names, so off-fork/apex consumers and the api-check resolve transparently, no cascade,
//     signature unchanged. This is exactly a keep-name model, applied to one subtree of a rename lane.
//   • REPOINT the subtree's outgoing refs to the lane's ALREADY-renamed modules (api references
//     framework-location, which the lane renamed to Holo2framework-location) — AST-safe via
//     renameAndRepointBp, derived-suffix aware. The subtree's OWN module names are protected: a base
//     the subtree defines is dropped from the rename map, so the fork can never rename its own modules.
//
// After this the lane loads its own keep-name api copy (finder per-file replacement drops the stock
// parallel), the in-fork consumers resolve framework-non-updatable-unbundled-defaults against it, and the
// lane depends on nothing in stock frameworks/packages. A post-create pass (like keepname-offork /
// refresh-manifest), so both holo2 and nexusm run this one implementation. Dry-run by default; -apply writes.
func cmdForkSharedInfra(args []string) int {
	fset := flag.NewFlagSet("fork-shared-infra", flag.ExitOnError)
	name := fset.String("name", "", "lane name (e.g. holo2)")
	out := fset.String("out", "", "AOSP root")
	subtree := fset.String("subtree", "", "stock subtree to fork keep-name (e.g. frameworks/base/api)")
	apply := fset.Bool("apply", false, "write the fork + ref repoints (default: dry-run — report only)")
	_ = fset.Parse(args)
	if *name == "" || *out == "" || *subtree == "" {
		fmt.Fprintln(os.Stderr, "fork-shared-infra: -name, -out and -subtree are required")
		return 2
	}
	sub := strings.Trim(filepath.ToSlash(strings.TrimSpace(*subtree)), "/")
	camel := strings.ToUpper((*name)[:1]) + (*name)[1:]
	c := deriveLane(*name, false, nil, false, false, camel)

	laneRel, ok := laneDirFor(mapPrefixedPath(c, sub), c.Name)
	if !ok {
		fmt.Fprintf(os.Stderr, "fork-shared-infra: subtree %q must be under frameworks/ or packages/\n", sub)
		return 2
	}
	stockDir := filepath.Join(*out, sub)
	if fi, err := os.Stat(stockDir); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "fork-shared-infra: stock subtree %s not found under -out\n", sub)
		return 2
	}

	// The rename map: every module the lane ALREADY renamed (Camel<base> → base), MINUS the names this
	// subtree itself defines. Excluding the subtree's own names is what makes the fork keep-name — a base
	// the subtree declares is never a rename key, so renameAndRepointBp cannot rename the subtree's module,
	// only repoint its refs to OTHER modules the lane renamed.
	subtreeDefs := bpModuleNamesUnder(stockDir)
	rename := laneRenameReverseMap(*out, *name, camel, subtreeDefs)

	// Preview the repoints against the STOCK subtree bps (identical to the forked copy, keep-name), so a
	// dry-run reports exactly what -apply will write without touching the tree.
	type hit struct{ path string }
	var repointed []string
	fileCount := 0
	_ = filepath.WalkDir(stockDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			fileCount++
		}
		if d.IsDir() || !isBpFile(d.Name()) {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		if _, ch, ferr := renameAndRepointBp(b, rename, *name); ferr == nil && ch {
			rel, _ := filepath.Rel(*out, p)
			repointed = append(repointed, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(repointed)

	fmt.Printf("fork-shared-infra: %s → %s (keep-name), lane %q\n", sub, laneRel, *name)
	fmt.Printf("  files to fork: %d   bps whose refs repoint to lane renames: %d\n", fileCount, len(repointed))
	for i, r := range repointed {
		if i >= 12 {
			fmt.Printf("    ... and %d more\n", len(repointed)-12)
			break
		}
		fmt.Printf("    repoint: %s\n", r)
	}
	if !*apply {
		fmt.Println("(dry-run; pass -apply to fork + repoint)")
		return 0
	}

	// Apply: (1) mirror the subtree keep-name (no-clobber), (2) repoint the forked bps' refs.
	laneRelMirrored, copied, err := mirrorSubtree(c, *out, sub)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fork-shared-infra: mirror failed: %v\n", err)
		return 1
	}
	changed, failed := 0, 0
	_ = filepath.WalkDir(filepath.Join(*out, laneRelMirrored), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isBpFile(d.Name()) {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		fi, _ := os.Stat(p)
		outB, ch, ferr := renameAndRepointBp(b, rename, *name)
		if ferr != nil {
			failed++
			return nil
		}
		if ch {
			mode := os.FileMode(0o644)
			if fi != nil {
				mode = fi.Mode()
			}
			if werr := os.WriteFile(p, outB, mode); werr == nil {
				changed++
			} else {
				failed++
			}
		}
		return nil
	})
	fmt.Printf("fork-shared-infra: applied — %d file(s) forked, %d bp(s) repointed, %d failed\n", copied, changed, failed)
	if failed > 0 {
		return 1
	}
	return 0
}

// cmdRepointLane re-applies the lane's rename map (renameAndRepointBp) across already-forked lane bps,
// WITHOUT mirroring or renaming any new definition. Its purpose is to re-run the repoint after the pass
// itself was improved — e.g. the nested-Map descent that reaches a `defaults:` list inside a
// soong_config_module_type's soong_config_variables{…conditions_default{…}} (the telecom straggler:
// framework-telecom-soong-availability-defaults still naming stock framework-telecom-updatable-defaults
// after the lane renamed it). The rename map is reconstructed from what is already renamed on disk, so a
// module's own name is never a key (bases only) — this repoints REFS, it does not rename definitions.
// Idempotent (already-repointed refs are unchanged) and writes only bps that actually change. -subtree
// scopes it to one already-forked lane dir to bound blast radius; default is the whole lane. Dry-run default.
func cmdRepointLane(args []string) int {
	fset := flag.NewFlagSet("repoint-lane", flag.ExitOnError)
	name := fset.String("name", "", "lane name (e.g. holo2)")
	out := fset.String("out", "", "AOSP root")
	subtree := fset.String("subtree", "", "optional lane subtree to scope to (e.g. packages-holo2/modules/Telephony/telecom)")
	only := fset.String("only", "", "optional comma-separated stock base names to restrict the rename map to (e.g. framework-telecom-updatable-defaults) — repoint ONLY these refs, leave all others untouched")
	apply := fset.Bool("apply", false, "write the repoints (default: dry-run — report only)")
	_ = fset.Parse(args)
	if *name == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "repoint-lane: -name and -out are required")
		return 2
	}
	camel := strings.ToUpper((*name)[:1]) + (*name)[1:]
	rename := laneRenameReverseMap(*out, *name, camel, nil)
	if *only != "" {
		allow := map[string]bool{}
		for _, b := range strings.Split(*only, ",") {
			if b = strings.TrimSpace(b); b != "" {
				allow[b] = true
			}
		}
		restricted := map[string]string{}
		for base, full := range rename {
			if allow[base] {
				restricted[base] = full
			}
		}
		rename = restricted
	}

	var roots []string
	if *subtree != "" {
		roots = []string{strings.Trim(filepath.ToSlash(*subtree), "/")}
	} else {
		roots = []string{"frameworks-" + *name, "packages-" + *name}
		if ents, err := os.ReadDir(filepath.Join(*out, "device", "google")); err == nil {
			for _, e := range ents {
				if e.IsDir() && strings.HasSuffix(e.Name(), "-"+*name) {
					roots = append(roots, filepath.ToSlash(filepath.Join("device", "google", e.Name())))
				}
			}
		}
	}

	var changedPaths []string
	var changedLineReport []string
	failed := 0
	for _, root := range roots {
		_ = filepath.WalkDir(filepath.Join(*out, root), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !isBpFile(d.Name()) {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			outB, ch, ferr := renameAndRepointBp(b, rename, *name)
			if ferr != nil {
				failed++
				return nil
			}
			if !ch {
				return nil
			}
			rel, _ := filepath.Rel(*out, p)
			changedPaths = append(changedPaths, filepath.ToSlash(rel))
			if !*apply {
				for _, d := range changedLines(b, outB) {
					changedLineReport = append(changedLineReport, filepath.ToSlash(rel)+": "+d)
				}
			}
			if *apply {
				fi, _ := os.Stat(p)
				mode := os.FileMode(0o644)
				if fi != nil {
					mode = fi.Mode()
				}
				if werr := os.WriteFile(p, outB, mode); werr != nil {
					failed++
				}
			}
			return nil
		})
	}
	sort.Strings(changedPaths)
	fmt.Printf("repoint-lane: %d bp(s) with refs to repoint (lane %q)\n", len(changedPaths), *name)
	for i, r := range changedPaths {
		if i >= 20 {
			fmt.Printf("  ... and %d more\n", len(changedPaths)-20)
			break
		}
		fmt.Printf("  %s\n", r)
	}
	if !*apply && len(changedLineReport) > 0 {
		fmt.Printf("  --- ref changes (%d) ---\n", len(changedLineReport))
		for i, d := range changedLineReport {
			if i >= 60 {
				fmt.Printf("    ... and %d more\n", len(changedLineReport)-60)
				break
			}
			fmt.Printf("    %s\n", d)
		}
	}
	if !*apply {
		fmt.Println("(dry-run; pass -apply to write)")
		return 0
	}
	fmt.Printf("repoint-lane: applied — %d bp(s) written, %d failed\n", len(changedPaths), failed)
	if failed > 0 {
		return 1
	}
	return 0
}

// changedLines returns the trimmed content lines that differ between old and new (form-preserving
// parser.Print changes only repointed tokens, so this stays tight). Used by the dry-run to show the
// exact ref rewrites for verification before touching the no-revert lane tree.
func changedLines(oldB, newB []byte) []string {
	oldL := strings.Split(string(oldB), "\n")
	newL := strings.Split(string(newB), "\n")
	var out []string
	n := len(oldL)
	if len(newL) < n {
		n = len(newL)
	}
	for i := 0; i < n; i++ {
		if oldL[i] != newL[i] {
			out = append(out, strings.TrimSpace(oldL[i])+"  ->  "+strings.TrimSpace(newL[i]))
		}
	}
	return out
}

// cmdIgnoreMissingLatestApi adds `unsafe_ignore_missing_latest_api: true` to every RENAMED java_sdk_library
// (Camel-prefixed) that lacks it. A java_sdk_library auto-depends on <name>.api[.combined].<scope>.latest —
// the "last released API" baseline, generated by prebuilts/sdk's prebuilt_apis under the STOCK module name.
// A renamed lane library (Holo2javax.obex) looks for a Holo2-named baseline nothing generates, so soong
// errors ("depends on undefined module …api.combined.public.latest") unless this flag is set (sdk_library.go
// gates the whole missing-latest-api check on it). The lane isn't release-tracking, so ignoring the missing
// baseline is correct. A rename-completeness pass, so both lanes run it. Dry-run default; -apply writes.
func cmdIgnoreMissingLatestApi(args []string) int {
	fset := flag.NewFlagSet("ignore-missing-latest-api", flag.ExitOnError)
	name := fset.String("name", "", "lane name (e.g. holo2)")
	out := fset.String("out", "", "AOSP root")
	apply := fset.Bool("apply", false, "write the property (default: dry-run — report only)")
	_ = fset.Parse(args)
	if *name == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "ignore-missing-latest-api: -name and -out are required")
		return 2
	}
	camel := strings.ToUpper((*name)[:1]) + (*name)[1:]
	const prop = "unsafe_ignore_missing_latest_api"
	var touched []string
	failed := 0
	for _, root := range []string{"frameworks-" + *name, "packages-" + *name} {
		_ = filepath.WalkDir(filepath.Join(*out, root), func(p string, d fs.DirEntry, err error) error {
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
			changed := false
			for _, def := range file.Defs {
				mod, ok := def.(*parser.Module)
				if !ok || mod.Type != "java_sdk_library" {
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
				if !isLaneRenamed(nm, camel, *name) {
					continue
				}
				if setOrAddBool(mod, prop, true) {
					changed = true
				}
			}
			if !changed {
				return nil
			}
			rel, _ := filepath.Rel(*out, p)
			touched = append(touched, filepath.ToSlash(rel))
			if *apply {
				outB, perr := parser.Print(file)
				if perr != nil {
					failed++
					return nil
				}
				fi, _ := os.Stat(p)
				mode := os.FileMode(0o644)
				if fi != nil {
					mode = fi.Mode()
				}
				if werr := os.WriteFile(p, outB, mode); werr != nil {
					failed++
				}
			}
			return nil
		})
	}
	sort.Strings(touched)
	fmt.Printf("ignore-missing-latest-api: %d renamed java_sdk_library bp(s) get %s=true (lane %q)\n", len(touched), prop, *name)
	for i, r := range touched {
		if i >= 30 {
			fmt.Printf("  ... and %d more\n", len(touched)-30)
			break
		}
		fmt.Printf("  %s\n", r)
	}
	if !*apply {
		fmt.Println("(dry-run; pass -apply to write)")
		return 0
	}
	fmt.Printf("ignore-missing-latest-api: applied — %d bp(s), %d failed\n", len(touched), failed)
	if failed > 0 {
		return 1
	}
	return 0
}

// isBlueprint reports whether a filename is a Blueprint file. The lane repoint/discovery passes must
// process ALL .bp files, not just Android.bp: an Android.bp pulls in sibling .bp via `build = [...]`
// (StubLibraries.bp, ApiDocs.bp, AconfigFlags.bp, Ravenwood.bp), and those carry modules + module-name
// variables that need the same rename/repoint. Keying only on "Android.bp" silently skipped them.
func isBpFile(name string) bool { return strings.HasSuffix(name, ".bp") }

// bpModuleNamesUnder returns every module name declared by a Blueprint file anywhere under dir.
func bpModuleNamesUnder(dir string) map[string]bool {
	set := map[string]bool{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isBpFile(d.Name()) {
			return nil
		}
		names, _ := bpModules(p)
		for n := range names {
			set[n] = true
		}
		return nil
	})
	return set
}

// laneRenameReverseMap reconstructs the lane's rename map from what is ALREADY renamed on disk: every lane
// module named Camel<base> yields base → Camel<base>. Names in protect (the shared-infra subtree's own
// definitions) are omitted so a keep-name fork never renames its own modules. A base that is ALSO defined
// KEEP-NAME in the lane is omitted too: keepname-offork un-renames off-fork-referenced modules to keep-name,
// so if both <base> and Camel<base> exist the base is the real (kept) module — repointing refs to it or
// renaming its def would break the un-rename. So a base maps only when the lane has Camel<base> and NOT <base>.
func laneRenameReverseMap(outRoot, lane, camel string, protect map[string]bool) map[string]string {
	laneMods := laneModuleSet(outRoot, lane)
	m := map[string]string{}
	for mod := range laneMods {
		base, ok := laneRenamedBase(mod, camel, lane)
		if !ok || base == "" || protect[base] || laneMods[base] {
			continue
		}
		m[base] = mod
	}
	return m
}
