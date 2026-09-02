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
	"sort"
	"strings"
	"text/template"

	parser "github.com/abstractsrevenge/sovereign_lane_surgeon/internal/blueprint/parser"
)

// bpmirror.go — §23.1 step 3 (Phase 3), KEEP-NAME model. Clones user-selected stock subtrees into
// the lane tree (frameworks-<lane>/, packages-<lane>/) VERBATIM (bp + all sources — a fork is a
// full-subtree copy, evidenced by frameworks-holo co-locating res/AndroidManifest/assets) and
// writes the root soong_namespace decls.
//
// EVIDENCE (bp_snapshot.json, full holo eng, 2026-07-12): of 6193 modules in 2486 forked bp,
// 6170 (99.6%) KEEP their stock name — framework-res/framework/services/framework-minus-apex all
// verbatim. A keep-name clone therefore needs ZERO bp ref-rewriting: cloned modules keep stock
// names, so their refs already resolve, and the finder does per-file replacement. The bareify
// casualties (§11-12) came from a botched RENAME pass, not from cloning. Prefixing (17 installables
// + 6 libs, via overrides/stem) is a SEPARATE opt-in pass (T-directed 2026-07-12). So v1 mirror is
// pure stdlib; blueprint/parser is added only when that prefix pass needs AST renaming.

// defaultInfraExcludes returns the subtrees to leave stock when a lane forks frameworks/base.
// These are build/test infra + namespace-complex subtrees that break a naive keep-name fork
// (discovered by real builds): ravenwood + hoststubgen carry `neverallow`/host-tool rules a UX
// lane never customizes; SystemUI declares sub-namespaces with bidirectional qualified refs that
// the keep-name model can't repoint (the RENAME model forks + renames it instead, so it is only
// excluded for keep-name). Merged with any user -fork-exclude so users don't rediscover these
// one build at a time.
func defaultInfraExcludes(c LaneConfig) []string {
	forksBase := false
	for _, f := range c.Forks {
		if strings.HasPrefix(strings.TrimLeft(f, "/"), "frameworks/base") {
			forksBase = true
		}
	}
	var ex []string
	if forksBase {
		ex = []string{"frameworks/base/ravenwood", "frameworks/base/tools/hoststubgen"}
		if c.KeepName {
			ex = append(ex, "frameworks/base/packages/SystemUI")
		} else {
			ex = append(ex, "frameworks/base/api")
		}
	}
	// NOTE: the Compose/AndroidX/Jetpack "vendored" excludes are NO LONGER unconditionally appended here
	// (a stale pasted literal that made even a non-frameworks/base fork carry ~17 excludes). They are now
	// OPT-IN under the -no-compose mode and COMPUTED from the actual tree at scaffold time via
	// computeVendoredExcludes(aospRoot) — merged at the scaffold call site, not here.
	return ex
}

// computeVendoredExcludes walks the stock frameworks/ + packages/ trees under aospRoot and returns the
// directories of every Android.bp that declares an AndroidX/Compose/Kotlin-vendored module (module
// name starts "androidx."/"kotlin" or contains "compose"). This replaces the stale hardcoded list with
// a value computed from the real tree, so the -no-compose lane leaves stock exactly the Compose/AndroidX
// subtrees it will not build (they resolve against un-forked stock). Same predicate as the standalone
// cmd/bpvendorexcludegen generator — that tool remains for ad-hoc listing; this is the wired runtime path.
func computeVendoredExcludes(aospRoot string) []string {
	seen := map[string]bool{}
	for _, d := range []string{"frameworks", "packages"} {
		filepath.Walk(filepath.Join(aospRoot, d), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || filepath.Base(path) != "Android.bp" {
				return nil
			}
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			file, errs := parser.Parse("", bytes.NewReader(b))
			if len(errs) > 0 {
				return nil
			}
			for _, def := range file.Defs {
				mod, ok := def.(*parser.Module)
				if !ok {
					continue
				}
				for _, prop := range mod.Properties {
					if prop.Name != "name" {
						continue
					}
					ns, ok := prop.Value.(*parser.String)
					if !ok {
						continue
					}
					n := strings.ToLower(ns.Value)
					if strings.HasPrefix(n, "androidx.") || strings.HasPrefix(n, "kotlin") || strings.Contains(n, "compose") {
						if rel, e := filepath.Rel(aospRoot, filepath.Dir(path)); e == nil {
							seen[rel] = true
						}
					}
				}
			}
			return nil
		})
	}
	out := make([]string, 0, len(seen))
	for dir := range seen {
		out = append(out, dir)
	}
	sort.Strings(out)
	return out
}

// computeComposeModuleNames walks stock frameworks/ + packages/ and returns the set of module names
// that are JETPACK COMPOSE modules (name contains "compose", case-insensitive). Deliberately NARROWER
// than computeVendoredExcludes' predicate: it does NOT include "kotlin"/"androidx." names, because a
// no-Compose lane still needs kotlin-stdlib and vendors AndroidX IN-APP (per lane doctrine) — only
// Compose itself is bypassed. Used to auto-drop dangling Compose dep refs from the mirrored lane bps.
func computeComposeModuleNames(aospRoot string) map[string]bool {
	names := map[string]bool{}
	for _, d := range []string{"frameworks", "packages"} {
		filepath.Walk(filepath.Join(aospRoot, d), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || filepath.Base(path) != "Android.bp" {
				return nil
			}
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			file, errs := parser.Parse("", bytes.NewReader(b))
			if len(errs) > 0 {
				return nil
			}
			for _, def := range file.Defs {
				mod, ok := def.(*parser.Module)
				if !ok {
					continue
				}
				for _, prop := range mod.Properties {
					if prop.Name != "name" {
						continue
					}
					if ns, ok := prop.Value.(*parser.String); ok && strings.Contains(strings.ToLower(ns.Value), "compose") {
						names[ns.Value] = true
					}
				}
			}
			return nil
		})
	}
	return names
}

// runNoComposeDropDeps drops dangling COMPOSE dep refs from every lane bp (frameworks-<lane>/,
// packages-<lane>/). After the Compose subtrees are left stock (computeVendoredExcludes) the mirrored
// lane bps still carry static_libs/libs refs to Compose modules the lane no longer builds → undefined.
// This is the auto form of the `drop-dep` subcommand for the -no-compose premise; it uses dropDepsBp
// (AST-safe, form-preserving), dropping bare-name and ":name" Compose refs. LIMIT: fully-qualified
// "//<lane>/path:ComposeLib" labels are not matched by name alone — a lane that carries those needs an
// explicit `drop-dep -deps` pass (documented). Returns (changed, failed) file counts.
func runNoComposeDropDeps(c LaneConfig, outRoot string) (changed, failed int) {
	if !c.NoCompose {
		return 0, 0
	}
	drop := computeComposeModuleNames(outRoot)
	if len(drop) == 0 {
		return 0, 0
	}
	fmt.Printf("\n-no-compose: dropping dangling Compose dep refs (%d Compose module names) across the lane tree:\n", len(drop))
	for _, root := range []string{"frameworks-" + c.Name, "packages-" + c.Name} {
		rootDir := filepath.Join(outRoot, root)
		if fi, err := os.Stat(rootDir); err != nil || !fi.IsDir() {
			continue
		}
		filepath.Walk(rootDir, func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			out, ch, derr := dropDepsBp(b, drop)
			if derr != nil {
				failed++
				return nil
			}
			if ch {
				os.WriteFile(p, out, 0o644)
				changed++
			}
			return nil
		})
	}
	fmt.Printf("  %d lane bp updated, %d unparseable.\n", changed, failed)
	return changed, failed
}

// runNoComposeScopeSystemUISrcs scopes SystemUI-class mirrored bps to the re-authored View-in-Kotlin
// kotlin/ tree — automating what the live NexusMSystemUI/Android.bp does by hand: srcs → ["kotlin/**/*.kt"]
// and drop Compose static_libs. It targets only lane bps whose directory basename contains "SystemUI"
// AND that carry a kotlin/ subtree (proof the Compose-free re-authoring exists). NO-OP when there is no
// kotlin/ tree — the toggle wires the BUILD around a re-authored tree, it does not generate the Kotlin.
// LIMIT: any narrow non-Compose java include the lane still needs (e.g. wallpapers) must be re-added by
// the operator; every rewrite is logged for review.
func runNoComposeScopeSystemUISrcs(c LaneConfig, outRoot string) (changed int) {
	if !c.NoCompose {
		return 0
	}
	for _, root := range []string{"frameworks-" + c.Name, "packages-" + c.Name} {
		rootDir := filepath.Join(outRoot, root)
		if fi, err := os.Stat(rootDir); err != nil || !fi.IsDir() {
			continue
		}
		filepath.Walk(rootDir, func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
				return nil
			}
			dir := filepath.Dir(p)
			if !strings.Contains(filepath.Base(dir), "SystemUI") {
				return nil
			}
			if kfi, err := os.Stat(filepath.Join(dir, "kotlin")); err != nil || !kfi.IsDir() {
				return nil // no re-authored kotlin/ tree → nothing to scope
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			out, ch, serr := scopeSystemUISrcsBp(b)
			if serr != nil || !ch {
				return nil
			}
			os.WriteFile(p, out, 0o644)
			changed++
			fmt.Printf("  -no-compose: scoped %s → srcs:[kotlin/**/*.kt] + dropped Compose static_libs (verify narrow java includes)\n", strings.TrimPrefix(p, outRoot+"/"))
			return nil
		})
	}
	return changed
}

// scopeSystemUISrcsBp rewrites every android_app/android_library in a bp: srcs → ["kotlin/**/*.kt"]
// (unless already exactly that), and removes static_libs entries whose name contains "compose".
func scopeSystemUISrcsBp(content []byte) ([]byte, bool, error) {
	file, errs := parser.Parse("", bytes.NewReader(content))
	if len(errs) > 0 {
		return nil, false, errs[0]
	}
	changed := false
	for _, def := range file.Defs {
		mod, ok := def.(*parser.Module)
		if !ok || (mod.Type != "android_app" && mod.Type != "android_library") {
			continue
		}
		for _, pr := range mod.Properties {
			lst, ok := pr.Value.(*parser.List)
			if !ok {
				continue
			}
			switch pr.Name {
			case "srcs":
				already := len(lst.Values) == 1
				if already {
					if s, ok := lst.Values[0].(*parser.String); !ok || s.Value != "kotlin/**/*.kt" {
						already = false
					}
				}
				if !already {
					lst.Values = []parser.Expression{&parser.String{Value: "kotlin/**/*.kt"}}
					changed = true
				}
			case "static_libs":
				kept := lst.Values[:0]
				for _, e := range lst.Values {
					if s, ok := e.(*parser.String); ok && strings.Contains(strings.ToLower(s.Value), "compose") {
						changed = true
						continue
					}
					kept = append(kept, e)
				}
				lst.Values = kept
			}
		}
	}
	if !changed {
		return content, false, nil
	}
	out, err := parser.Print(file)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// mergeDedup unions two path lists, preserving order and dropping duplicates.
func mergeDedup(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string{}, a...), b...) {
		if s = strings.TrimLeft(s, "/"); s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// checkLaneSuffixCollision reports existing directories whose NAME ends in "-<lane>". Such a
// directory is indistinguishable from lane content to the finder's suffix predicates, so every
// lane in the tree would stop scanning it.
//
// This is not hypothetical. A lane named "test" makes isOtherLaneBp drop every directory ending
// in "-test" — 107 of them in AOSP, 15 carrying an Android.bp, including
// prebuilts/misc/common/androidx-test. The symptom is remote from the cause: unrelated lanes fail
// with "depends on undefined module androidx.test.core" from external/robolectric, and nothing in
// that error mentions the new lane. The name is the defect, and it must be rejected before any
// file is written, because by the time a build says otherwise the tree already holds a multi-GB
// clone and patched shared soong sources.
//
// Returns offenders (capped) and the total count. Skips out*/.git/.repo and the lane's own roots.
func checkLaneSuffixCollision(outRoot, lane string) (offenders []string, total int) {
	suffix := "-" + lane
	ownFw, ownPkg := "frameworks"+suffix, "packages"+suffix
	for _, top := range []string{"frameworks", "packages", "external", "prebuilts", "system", "device", "build", "art", "libcore", "tools", "cts", "development", "hardware", "bootable", "vendor"} {
		root := filepath.Join(outRoot, top)
		if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
			continue
		}
		filepath.Walk(root, func(p string, fi os.FileInfo, e error) error {
			if e != nil || !fi.IsDir() {
				return nil
			}
			base := filepath.Base(p)
			if base == ".git" || base == ".repo" || strings.HasPrefix(base, "out") && filepath.Dir(p) == outRoot {
				return filepath.SkipDir
			}
			if base == ownFw || base == ownPkg {
				return filepath.SkipDir
			}
			if strings.HasSuffix(base, suffix) {
				total++
				if len(offenders) < 8 {
					rel, _ := filepath.Rel(outRoot, p)
					offenders = append(offenders, rel)
				}
			}
			return nil
		})
	}
	return offenders, total
}

// laneDirFor maps a fork-source subtree to its lane-dir parallel. The source may be stock
// (frameworks/base) or ANOTHER LANE (frameworks-holo/base) — a lane-sourced fork seeds the new
// lane from a proven lane instead of stock, so it inherits that lane's UX rather than re-deriving
// it. Only the FIRST path segment is rewritten, and it is replaced whole: a naive
// strings.Replace(p, "frameworks", ...) turns "frameworks-holo/base" into "frameworks-test-holo/base".
// Matching the whole segment is also what keeps this separator-aware.
//
// NOTE: a lane-sourced clone is verbatim (keep-name, no ref rewrite), so the source lane's
// qualified //frameworks-<src>/… labels come across pointing at the SOURCE lane — which the new
// lane's finder drops. Run `requalify -name <new> -from <src>` after the mirror to repoint them.
func laneDirFor(srcSubtree, lane string) (laneRel string, ok bool) {
	seg, tail := srcSubtree, ""
	if i := strings.IndexByte(srcSubtree, '/'); i >= 0 {
		seg, tail = srcSubtree[:i], srcSubtree[i:]
	}
	for _, root := range []string{"frameworks", "packages"} {
		if seg == root || strings.HasPrefix(seg, root+"-") {
			return root + "-" + lane + tail, true
		}
	}
	return "", false
}

// mapPrefixedPath applies the DirPrefix to the top-level app/package directory in the path.
func mapPrefixedPath(c LaneConfig, stockPath string) string {
	if c.DirPrefix == "" {
		return stockPath
	}
	parts := strings.Split(filepath.ToSlash(stockPath), "/")
	prefixIndex := -1
	if len(parts) >= 3 && parts[0] == "packages" && parts[1] == "apps" {
		prefixIndex = 2
	} else if len(parts) >= 4 && parts[0] == "frameworks" && parts[1] == "base" && parts[2] == "packages" {
		prefixIndex = 3
	}
	if prefixIndex != -1 && prefixIndex < len(parts) {
		if !strings.HasPrefix(strings.ToLower(parts[prefixIndex]), strings.ToLower(c.DirPrefix)) {
			parts[prefixIndex] = c.DirPrefix + parts[prefixIndex]
		}
	}
	return filepath.Join(parts...)
}

// mirrorSubtree recursively clones outRoot/<stockSubtree> → outRoot/<lane-parallel>, verbatim,
// no-clobber per file (an already-forked file is authored once — never overwrite). Returns the
// count of files copied.
func mirrorSubtree(c LaneConfig, outRoot, stockSubtree string) (laneRel string, copied int, err error) {
	laneRel, ok := laneDirFor(mapPrefixedPath(c, stockSubtree), c.Name)
	if !ok {
		return "", 0, fmt.Errorf("subtree %q must be under frameworks/ or packages/", stockSubtree)
	}
	src := filepath.Join(outRoot, stockSubtree)
	info, err := os.Stat(src)
	if err != nil {
		return "", 0, fmt.Errorf("stock subtree %s not found under -out: %w", stockSubtree, err)
	}
	if !info.IsDir() {
		return "", 0, fmt.Errorf("%s is not a directory", stockSubtree)
	}
	err = filepath.Walk(src, func(p string, fi os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		// Skip VCS metadata — not lane content (a real subtree like frameworks/base carries a
		// .git worktree symlink/dir; copying it is wrong and would otherwise abort the walk).
		if base := filepath.Base(p); base == ".git" || base == ".repo" {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		// Fork-boundary exclusion: leave namespace-complex subpaths (e.g. .../SystemUI) stock so
		// their qualified labels resolve to the un-dropped stock bp (bidirectional refs).
		stockRel := filepath.Join(stockSubtree, rel)
		for _, ex := range c.ForkExclude {
			if stockRel == ex || strings.HasPrefix(stockRel, ex+"/") {
				if fi.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		mappedStockRel := mapPrefixedPath(c, stockRel)
		laneRelMapped, _ := laneDirFor(mappedStockRel, c.Name)
		target := filepath.Join(outRoot, laneRelMapped)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		// Preserve symlinks (e.g. frameworks/base/api/docs -> ../docs) — relative links stay valid
		// within the fork; skipping them breaks modules whose srcs resolve through the link.
		if fi.Mode()&os.ModeSymlink != 0 {
			link, lerr := os.Readlink(p)
			if lerr != nil {
				return nil
			}
			if _, serr := os.Lstat(target); serr == nil {
				return nil // no-clobber
			}
			if merr := os.MkdirAll(filepath.Dir(target), 0o755); merr != nil {
				return merr
			}
			if os.Symlink(link, target) == nil {
				copied++
			}
			return nil
		}
		// Other non-regular files (sockets, devices) aren't lane source — skip.
		if !fi.Mode().IsRegular() {
			return nil
		}
		if _, serr := os.Stat(target); serr == nil || neutralizedByBak(target) {
			return nil // no-clobber: keep the already-forked file
		}
		if cerr := copyFile(p, target); cerr != nil {
			return cerr
		}
		copied++
		return nil
	})
	return laneRel, copied, err
}

var rootNamespaceTmpl = template.Must(template.New("rootns").Parse(`//
// Copyright 2026 The Android Open-Source Project
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

// {{.Camel}} lane root namespace decl. The finder DROPS this decl for a {{.Lane}} build (modules
// collapse to the global namespace, matching stock topology — §2 rule 2) while keeping the stock
// parallel for shared consumers; the decl must exist for non-{{.Lane}} builds that import by path.
soong_namespace {
    imports: [{{range $i, $imp := .Imports}}{{if $i}}, {{end}}"{{$imp}}"{{end}}],
}

package {
    default_visibility: ["//visibility:public"],
}
`))

// renderRootNamespace produces a lane-root Android.bp (soong_namespace + public package).
func renderRootNamespace(lane, camel string, imports []string) (string, error) {
	var sb strings.Builder
	err := rootNamespaceTmpl.Execute(&sb, struct {
		Lane, Camel string
		Imports     []string
	}{lane, camel, imports})
	return sb.String(), err
}

// ensureRootNamespace writes the root Android.bp for whichever lane roots were populated:
// frameworks-<lane> (imports []) and/or packages-<lane> (imports [frameworks-<lane>]).
func ensureRootNamespace(c LaneConfig, outRoot string, roots map[string]bool) (wrote, skipped int, fatal bool) {
	type spec struct {
		dir     string
		imports []string
	}
	var specs []spec
	if roots["frameworks"] {
		specs = append(specs, spec{"frameworks-" + c.Name, nil})
	}
	if roots["packages"] {
		specs = append(specs, spec{"packages-" + c.Name, []string{"frameworks-" + c.Name}})
	}
	for _, s := range specs {
		content, err := renderRootNamespace(c.Name, c.CamelCase, s.imports)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! render root namespace %s: %v\n", s.dir, err)
			return wrote, skipped, true
		}
		w, sk := writeIfAbsent(outRoot, filepath.Join(s.dir, "Android.bp"), content)
		wrote += w
		skipped += sk
	}
	return wrote, skipped, false
}

// runMirror clones each --fork subtree into the lane tree + writes the root namespace decls.
func runMirror(c LaneConfig, outRoot string, forks []string) (copied, nsWrote int, fatal bool) {
	roots := map[string]bool{}
	fmt.Printf("\nbp-mirror (keep-name — verbatim clone, no ref rewrite):\n")
	for _, fk := range forks {
		fk = strings.Trim(strings.TrimSpace(fk), "/")
		if fk == "" {
			continue
		}
		laneRel, n, err := mirrorSubtree(c, outRoot, fk)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! %v\n", err)
			continue
		}
		if strings.HasPrefix(fk, "frameworks") {
			roots["frameworks"] = true
		} else {
			roots["packages"] = true
		}
		fmt.Printf("  + %s → %s (%d files cloned)\n", fk, laneRel, n)
		copied += n
	}
	w, _, fatalNs := ensureRootNamespace(c, outRoot, roots)
	nsWrote = w
	return copied, nsWrote, fatalNs
}
