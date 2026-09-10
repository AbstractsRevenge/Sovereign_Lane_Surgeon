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
	"regexp"
	"sort"
	"strings"
)

// cmdRepointRustUse repoints Rust SOURCE crate references (`use <crate>::…`, `<crate>::…`) from a
// stock crate name to its lane-renamed name — the rust-source ANALOG of the bp dep-ref repoint.
//
// Why a separate pass: the rename pass repoints Blueprint dep refs (rustlibs/static_libs/…) in
// lockstep with a module rename, but a Rust crate reference lives in SOURCE (`use servicemanager_aidl`)
// where no Blueprint edit reaches it. When the lane renames an aidl_interface/rust_library it OWNS and
// repoints every bp consumer, `m nothing` stays green (bp graph consistent) — but a lane rust file that
// `use`s the crate by its old name fails to COMPILE ("unresolved module or unlinked crate"), invisible
// until rustc runs. The generated rlib is exposed to `use` under the NEW crate name (rustc --extern
// <new>=…); the source must follow. This applies ONLY when the lane owns every consumer (rename+repoint);
// a crate with a legit off-fork/stock-name consumer must KEEP-NAME instead (rename-module -deprefix).
//
// Byte-safe: matches only a whole-identifier crate token immediately followed by "::", with a
// non-identifier char (or start) before it — so it never fires inside a longer identifier
// (rpc_servicemanager_aidl vs servicemanager_aidl) nor on an already-applied Holo2-prefixed token.
// Pairs are applied longest-old-first as belt-and-suspenders against substring overlap.
func cmdRepointRustUse(args []string) int {
	fset := flag.NewFlagSet("repoint-rust-use", flag.ExitOnError)
	name := fset.String("name", "", "lane name, lowercase (e.g. holo2)")
	out := fset.String("out", "", "AOSP root holding frameworks-<lane>/ and packages-<lane>/")
	mapFlag := fset.String("map", "", "crate rename map: old=new,old2=new2 (stock crate name = lane crate name)")
	apply := fset.Bool("apply", false, "apply the repoint (default: dry-run — print the plan)")
	_ = fset.Parse(args)
	if *name == "" || *out == "" || *mapFlag == "" {
		fmt.Fprintln(os.Stderr, "repoint-rust-use: -name <lane>, -out <aosp-root> and -map old=new,… are required")
		return 2
	}

	type pair struct{ old, new string }
	var pairs []pair
	for _, kv := range strings.Split(*mapFlag, ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		i := strings.IndexByte(kv, '=')
		if i <= 0 || i == len(kv)-1 {
			fmt.Fprintf(os.Stderr, "repoint-rust-use: bad -map entry %q (want old=new)\n", kv)
			return 2
		}
		o, n := strings.TrimSpace(kv[:i]), strings.TrimSpace(kv[i+1:])
		if !isRustCrateIdent(o) || !isRustCrateIdent(n) {
			fmt.Fprintf(os.Stderr, "repoint-rust-use: %q or %q is not a bare crate identifier\n", o, n)
			return 2
		}
		pairs = append(pairs, pair{o, n})
	}
	if len(pairs) == 0 {
		fmt.Fprintln(os.Stderr, "repoint-rust-use: -map parsed to zero pairs")
		return 2
	}
	// Longest old first: guarantees rpc_servicemanager_aidl is rewritten before servicemanager_aidl,
	// so the shorter token can never clobber the inside of the longer one even without the regex guard.
	sort.SliceStable(pairs, func(i, j int) bool { return len(pairs[i].old) > len(pairs[j].old) })

	// Existence guard: the NEW crate should be defined by the lane (an aidl_interface / rust_library
	// module of that name under a lane root). A -map typo that points at a nonexistent crate would
	// otherwise silently rewrite good source into an unbuildable state. Warn — never rewrite blind.
	// Computed ONCE here (a single combined lane-bp walk), never inside the per-file loop below.
	laneRoots := []string{
		filepath.Join(*out, "frameworks-"+*name),
		filepath.Join(*out, "packages-"+*name),
	}
	// The lane's crate namespace: every module name a lane Android.bp declares, SANITIZED to its Rust
	// crate form (dots/dashes → underscores). A dotted aidl_interface `Holo2android.system.foo` yields
	// rust crate `Holo2android_system_foo`, so the guard must compare the crate form, not the raw name.
	laneCrates := laneModuleCrateSet(laneRoots)
	valid := make([]bool, len(pairs))
	for i, p := range pairs {
		valid[i] = laneCrates[p.new]
		if !valid[i] {
			fmt.Printf("  ! new crate %q is not a lane module's crate name — check the -map; skipping this pair\n", p.new)
		}
	}

	// Per pair, a matcher: (start|non-ident) ( old ) (::|\s+as\b) — capture the boundary and trailing delimiter so they are preserved.
	matchers := make([]*regexp.Regexp, len(pairs))
	for i, p := range pairs {
		matchers[i] = regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(p.old) + `(::|\s+as\b)`)
	}

	totalFiles, totalHits := 0, 0
	for _, root := range laneRoots {
		if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
			continue
		}
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".rs") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			s := string(b)
			fileHits := 0
			for i, p := range pairs {
				if !valid[i] {
					continue // guarded-out pair
				}
				// Count via the boundary-aware matcher BEFORE replacing: the replacement
				// Holo2<old>:: still contains <old>:: as a substring, so a strings.Count delta
				// would read zero. FindAllStringIndex counts the real, whole-token matches.
				n := len(matchers[i].FindAllStringIndex(s, -1))
				if n == 0 {
					continue
				}
				fileHits += n
				s = matchers[i].ReplaceAllString(s, "${1}"+p.new+"${2}")
			}
			if fileHits == 0 {
				return nil
			}
			rel, _ := filepath.Rel(*out, path)
			totalFiles++
			totalHits += fileHits
			if *apply {
				if os.WriteFile(path, []byte(s), info.Mode().Perm()) == nil {
					fmt.Printf("  + %-70s %d ref(s) repointed\n", rel, fileHits)
				}
			} else {
				fmt.Printf("  ~ %-70s %d ref(s) would repoint\n", rel, fileHits)
			}
			return nil
		})
	}

	verb := "would repoint"
	if *apply {
		verb = "repointed"
	}
	fmt.Printf("\nrepoint-rust-use: %s %d crate ref(s) across %d lane rust file(s)\n", verb, totalHits, totalFiles)
	if !*apply {
		fmt.Println("(dry-run — re-run with -apply to write)")
	}
	return 0
}

// isRustCrateIdent reports whether s is a bare Rust crate identifier (no path separators).
func isRustCrateIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		ok := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// nameDecl matches a Blueprint `name: "<value>"` property and captures the value.
var nameDecl = regexp.MustCompile(`\bname:\s*"([^"]+)"`)

// sanitizeCrate maps a Blueprint module name to its Rust crate identifier: every char that is not a
// Rust identifier char (dots and dashes in aidl_interface names, chiefly) becomes '_'. Mirrors how
// Soong derives an aidl rust crate name from the interface name.
func sanitizeCrate(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// laneModuleCrateSet returns the set of every lane module name, SANITIZED to its Rust crate form, in a
// SINGLE combined walk over the lane bps. Used as the existence guard for -map targets: a rename map
// that points at a crate no lane module produces is a typo, and must never rewrite source blind.
func laneModuleCrateSet(roots []string) map[string]bool {
	set := make(map[string]bool, 4096)
	for _, root := range roots {
		if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
			continue
		}
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || filepath.Base(path) != "Android.bp" {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			for _, m := range nameDecl.FindAllStringSubmatch(string(b), -1) {
				set[sanitizeCrate(m[1])] = true
			}
			return nil
		})
	}
	return set
}
