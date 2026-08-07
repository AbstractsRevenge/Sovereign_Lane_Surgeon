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
	_ = fs.Parse(args)
	if *name == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "requalify: -name and -out are required")
		return 2
	}
	laneMap := map[string]string{
		"frameworks": "frameworks-" + *name,
		"packages":   "packages-" + *name,
	}
	cache := map[string]bool{}
	var roots []string
	if *subtree != "" {
		roots = []string{*subtree}
	} else {
		roots = []string{"frameworks-" + *name, "packages-" + *name}
	}
	changed, failed := 0, 0
	fmt.Printf("requalify //<root>/… → //<root>-%s/… (AST-safe, forked targets only):\n", *name)
	for _, root := range roots {
		rootDir := filepath.Join(*out, root)
		if fi, err := os.Stat(rootDir); err != nil || !fi.IsDir() {
			continue
		}
		_ = filepath.Walk(rootDir, func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
				return nil
			}
			ch, ferr := requalifyFile(p, *out, laneMap, cache, *force, *prefix)
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
	return 0
}
