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
	"strings"
)

// cmdDropDep removes named dependency entries from every dep-name list (static_libs/libs/shared_libs/
// whole_static_libs/header_libs/runtime_libs/defaults/required/…) and every `:module` srcs ref across the
// lane tree, AST-safely (reuses dropDepsBp — the same Blueprint engine as the rename ops). Form-preserving;
// idempotent (an entry already absent is a no-op).
//
// PRIMARY USE — Model-A no-Compose: the lane's SystemUI component bps were stock-mirrored + delaned and so
// carry `static_libs: ["//<lane>/libs/systemui:X"]` external-module refs to libs the no-Compose lane does
// NOT vendor outward (they resolve internally via the app's own srcs / Dagger / FQN). Those stale refs point
// at empty lib dirs → undefined-module. Dropping the ref is the fix. Pass full labels or bare names in -deps.
func cmdDropDep(args []string) int {
	fs := flag.NewFlagSet("drop-dep", flag.ExitOnError)
	name := fs.String("name", "", "lane name (e.g. nexusm) — walks frameworks-<lane> + packages-<lane>")
	out := fs.String("out", "", "AOSP root")
	subtree := fs.String("subtree", "", "optional repo-relative subtree to limit the walk")
	deps := fs.String("deps", "", "comma-list of dep entries to remove (full label like //root/dir:mod, or bare name)")
	_ = fs.Parse(args)
	if *name == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "drop-dep: -name and -out are required")
		return 2
	}
	drop := map[string]bool{}
	for _, d := range strings.Split(*deps, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			drop[d] = true
		}
	}
	if len(drop) == 0 {
		fmt.Fprintln(os.Stderr, "drop-dep: -deps is required (comma-list)")
		return 2
	}

	var roots []string
	if *subtree != "" {
		roots = []string{*subtree}
	} else {
		roots = []string{"frameworks-" + *name, "packages-" + *name}
	}

	fmt.Printf("drop-dep (%d entr(y/ies), AST-safe — remove from dep lists + :module srcs):\n", len(drop))
	for d := range drop {
		fmt.Printf("  - %s\n", d)
	}
	changed, failed := 0, 0
	for _, root := range roots {
		rootDir := filepath.Join(*out, root)
		if fi, err := os.Stat(rootDir); err != nil || !fi.IsDir() {
			continue
		}
		_ = filepath.Walk(rootDir, func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			outBytes, ch, ferr := dropDepsBp(b, drop)
			if ferr != nil {
				failed++
				return nil
			}
			if ch {
				if werr := os.WriteFile(p, outBytes, 0o644); werr == nil {
					changed++
					rel, _ := filepath.Rel(*out, p)
					fmt.Printf("  ✓ %s\n", rel)
				}
			}
			return nil
		})
	}
	fmt.Printf("drop-dep: %d bp updated, %d skipped (parse).\n", changed, failed)
	return 0
}
