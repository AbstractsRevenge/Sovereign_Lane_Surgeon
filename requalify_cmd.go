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
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// cmdRequalify runs the AST-safe //<root>/… → //<root>-<lane>/… delane over an EXISTING lane's
// Android.bp files (not just a fresh seed's mirror). Purpose: retire vestigial stock-path
// references (visibility grants, deps) that violate lane sovereignty — a lane must not reference
// stock //frameworks/base et al. Conservative: only repoints a label whose lane parallel dir
// exists; a no-parallel stock label is left untouched (handle those explicitly). Blueprint-AST +
// form-preserving (reuses requalifyFile). Scope with -subtree for a reviewed pass; review the
// result with `git diff` (revert with `git checkout` — the lane tree is under git).
func cmdRequalify(args []string) int {
	fs := flag.NewFlagSet("requalify", flag.ExitOnError)
	name := fs.String("name", "", "lane name (e.g. nexusm)")
	out := fs.String("out", "", "AOSP root")
	subtree := fs.String("subtree", "", "optional repo-relative subtree to limit the walk (default: frameworks-<lane> + packages-<lane>)")
	force := fs.Bool("force", false, "absolute-sovereignty: retarget stock labels to the lane path even when that lane parallel doesn't exist yet (grant to a not-yet-forked package is harmless + forward-compatible)")
	prefix := fs.String("prefix", "", "rename-model: prefix the app/package segment after apps//base/packages/ (e.g. -prefix NexusM → //packages/apps/Nfc → //packages-nexusm/apps/NexusMNfc)")
	from := fs.String("from", "", "source lane to repoint FROM (e.g. -from holo rewrites //frameworks-holo/… → //frameworks-<name>/…). Default: stock roots.")
	sources := fs.Bool("sources", false, "also relocate lane paths inside NON-blueprint sources (.proto imports, C/C++ #includes, .mk paths). Requires -from. These break the build from inside GENERATED files, so they are invisible to any Android.bp check.")
	paths := fs.Bool("paths", false, "also rewrite UNQUALIFIED root-relative paths (include_dirs, aidl.include_dirs, cmd fragments). Off by default: only //-qualified labels are touched. Required for a lane-sourced fork, where a bare path silently resolves against the source lane's on-disk files.")
	_ = fs.Parse(args)
	if *name == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "requalify: -name and -out are required")
		return 2
	}
	if *from == *name && *from != "" {
		fmt.Fprintln(os.Stderr, "requalify: -from and -name must differ (a lane cannot be requalified onto itself)")
		return 2
	}
	// srcSuffix selects which roots are REWRITTEN. Empty = stock (//frameworks/…), the original
	// delane. A -from lane makes this a lane→lane repoint, which is what a lane-sourced fork needs:
	// the verbatim clone carries the source lane's labels, and the new lane's finder drops that lane.
	srcSuffix := ""
	if *from != "" {
		srcSuffix = "-" + *from
	}
	laneMap := map[string]string{
		"frameworks" + srcSuffix: "frameworks-" + *name,
		"packages" + srcSuffix:   "packages-" + *name,
	}
	cache := map[string]bool{}
	var roots []string
	if *subtree != "" {
		roots = []string{*subtree}
	} else {
		roots = []string{"frameworks-" + *name, "packages-" + *name}
	}
	changed, failed := 0, 0
	fmt.Printf("requalify //<root>%s/… → //<root>-%s/… (AST-safe, forked targets only):\n", srcSuffix, *name)
	for _, root := range roots {
		rootDir := filepath.Join(*out, root)
		if fi, err := os.Stat(rootDir); err != nil || !fi.IsDir() {
			continue
		}
		_ = filepath.Walk(rootDir, func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
				return nil
			}
			ch, ferr := requalifyFile(p, *out, laneMap, cache, *force, *prefix, *paths)
			if ferr != nil {
				failed++
				return nil
			}
			if ch {
				changed++
				rel, _ := filepath.Rel(*out, p)
				fmt.Printf("  ✓ %s\n", rel)
			}
			return nil
		})
	}
	fmt.Printf("requalify: %d bp repointed, %d skipped (parse).\n", changed, failed)
	if *sources {
		// -sources used to REQUIRE -from. It no longer does: a STOCK-sourced lane has the same
		// problem (its cloned .proto files still import frameworks/…, so protoc emits a
		// stock-shaped #include into a generated .pb.h the lane-rooted -I cannot satisfy), it just
		// needs the existence-guarded pass instead of the unconditional one. See
		// relocateStockPathsInFile for why the stock direction cannot reuse the lane→lane token
		// replace. (holo2test, 2026-08-20.)
		if *from != "" {
			runRelocateLaneSourcePaths(LaneConfig{Name: *name}, *out, *from)
		} else {
			runRelocateStockSourcePaths(LaneConfig{Name: *name}, *out)
		}
	}
	return 0
}
