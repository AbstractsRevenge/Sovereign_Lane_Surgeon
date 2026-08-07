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

// cmdRenameModule applies an AST-safe module-name rewrite across a lane tree. For each old=new
// mapping it rewrites `name:"old"` → `name:"new"` AND repoints every bare dep-name reference
// (defaults/static_libs/libs/shared_libs/whole_static_libs/header_libs/runtime_libs/required/…) plus
// every `:old` filegroup ref in srcs, in ONE lockstep pass. Reuses renameAndRepointBp — the same
// Blueprint-AST engine the tier-2 library rename uses — so it is form-preserving and never a
// regex/brace-eating sweep (HARD RULE 3). Idempotent: a module already named `new` is not a map key,
// so it is untouched.
//
// PRIMARY USE — Model-A keep-name conformity DE-PREFIX. A sovereign lane may have wrongly PREFIXED a
// framework-class / shared-infra module (a java_defaults, a shared lib) that stock consumers across
// the tree reference by its BARE stock name. While the lane was finder-inert this was invisible; once
// the lane loads keep-name-style (framework-class collapsed to the global namespace, stock parallels
// dropped) those stock consumers see the bare name as undefined. This op de-prefixes such modules
// back to keep-name across the lane tree — the definition and every lane-internal consumer together.
// Stock consumers already use the bare name and need no change (they live outside the lane tree).
//
// FORMS:
//
//	-map "old1=new1,old2=new2"          fully general (any rename)
//	-deprefix <P> -modules a,b,c        ergonomic de-prefix: builds <P>a=a,<P>b=b,<P>c=c
//
// Walks frameworks-<lane> + packages-<lane> (or -subtree). Review with git diff (the lane tree is
// under git); revert with git checkout. Only modules whose OLD name actually appears are rewritten.
func cmdRenameModule(args []string) int {
	fs := flag.NewFlagSet("rename-module", flag.ExitOnError)
	name := fs.String("name", "", "lane name (e.g. nexusm) — walks frameworks-<lane> + packages-<lane>")
	out := fs.String("out", "", "AOSP root")
	subtree := fs.String("subtree", "", "optional repo-relative subtree to limit the walk")
	mapArg := fs.String("map", "", "explicit rename pairs: old1=new1,old2=new2")
	deprefix := fs.String("deprefix", "", "de-prefix form: the prefix to strip (pair with -modules)")
	modules := fs.String("modules", "", "de-prefix form: comma-list of BARE (stock) module names")
	_ = fs.Parse(args)
	if *name == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "rename-module: -name and -out are required")
		return 2
	}

	rename := map[string]string{}
	if *mapArg != "" {
		for _, pair := range strings.Split(*mapArg, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			kv := strings.SplitN(pair, "=", 2)
			if len(kv) != 2 || strings.TrimSpace(kv[0]) == "" || strings.TrimSpace(kv[1]) == "" {
				fmt.Fprintf(os.Stderr, "rename-module: bad -map pair %q (want old=new)\n", pair)
				return 2
			}
			rename[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	if (*deprefix != "") != (*modules != "") {
		fmt.Fprintln(os.Stderr, "rename-module: -deprefix and -modules must be given together")
		return 2
	}
	if *deprefix != "" {
		for _, m := range strings.Split(*modules, ",") {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			rename[*deprefix+m] = m
		}
	}
	if len(rename) == 0 {
		fmt.Fprintln(os.Stderr, "rename-module: nothing to do (pass -map or -deprefix + -modules)")
		return 2
	}

	var roots []string
	if *subtree != "" {
		roots = []string{*subtree}
	} else {
		roots = []string{"frameworks-" + *name, "packages-" + *name}
	}

	fmt.Printf("rename-module (%d mapping(s), AST-safe — def + dep-refs, form-preserving):\n", len(rename))
	for k, v := range rename {
		fmt.Printf("  %s → %s\n", k, v)
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
			outBytes, ch, ferr := renameAndRepointBp(b, rename, "")
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
	fmt.Printf("rename-module: %d bp updated, %d skipped (parse).\n", changed, failed)
	return 0
}
