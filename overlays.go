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
	"embed"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// overlays.go — Soong-conversion overlays (assets/overlays, see its MANIFEST) and the
// target-compat operation that applies them.
//
// WHY (observed android-17.0.0_r1, 2026-09-02): build/soong/ui/build/androidmk_denylist.go blocks
// every Android.mk under device/google/ and hardware/google/ ("Found blocked Android.mk file:
// hardware/google/graphics/common/Android.mk"), and android-15.0.0_r36's graphics/common still
// builds hwc3 and libhwc2.1 from makefiles. Google converted them to Soong upstream (562ede8); that
// conversion, and only it, is what the hand bring-up had to fetch. Here the converted files travel
// with the bundle as an overlay: when the target's denylist covers a mirrored Android.mk and an
// overlay exists for its subtree, the blocked makefiles are removed and the overlay's files are
// written; a blocked makefile with no overlay is reported as the blocker it is, never guessed at.
// A target that does not block the makefiles (android-15 itself) is left as mirrored.

//go:embed all:assets/overlays
var embeddedOverlays embed.FS

const (
	overlaysRoot         = "assets/overlays"
	androidmkDenylistRel = "build/soong/ui/build/androidmk_denylist.go"
)

type overlayEntry struct {
	Subtree string // repo-relative, forward-slashed
	Source  string // upstream@commit
	Note    string
}

// embeddedOverlayEntries parses assets/overlays/MANIFEST.
func embeddedOverlayEntries() []overlayEntry {
	b, err := embeddedOverlays.ReadFile(path.Join(overlaysRoot, "MANIFEST"))
	if err != nil {
		return nil
	}
	var out []overlayEntry
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		e := overlayEntry{Subtree: strings.Trim(f[0], "/"), Source: f[1]}
		if len(f) > 2 {
			e.Note = strings.Join(f[2:], " ")
		}
		out = append(out, e)
	}
	return out
}

// overlayFor returns the overlay whose subtree is the longest prefix of rel (forward-slashed).
func overlayFor(rel string, entries []overlayEntry) (overlayEntry, bool) {
	var best overlayEntry
	found := false
	for _, e := range entries {
		if rel == e.Subtree || strings.HasPrefix(rel, e.Subtree+"/") {
			if !found || len(e.Subtree) > len(best.Subtree) {
				best, found = e, true
			}
		}
	}
	return best, found
}

// soongAndroidmkDenylist reads androidmk_denylist / androidmk_allowlist out of the target's Soong
// with go/ast. A tree without the file blocks nothing.
func soongAndroidmkDenylist(outRoot string) (deny, allow []string, err error) {
	p := filepath.Join(outRoot, filepath.FromSlash(androidmkDenylistRel))
	f, perr := goparser.ParseFile(token.NewFileSet(), p, nil, 0)
	if perr != nil {
		return nil, nil, perr
	}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if i >= len(vs.Values) || (name.Name != "androidmk_denylist" && name.Name != "androidmk_allowlist") {
				continue
			}
			cl, ok := vs.Values[i].(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, el := range cl.Elts {
				lit, ok := el.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				s, uerr := strconv.Unquote(lit.Value)
				if uerr != nil || s == "" {
					continue
				}
				if name.Name == "androidmk_denylist" {
					deny = append(deny, s)
				} else {
					allow = append(allow, s)
				}
			}
		}
		return true
	})
	return deny, allow, nil
}

// androidmkBlocked mirrors Soong's rule: a makefile is blocked when its path starts with a
// denylisted prefix and is not explicitly allowlisted.
func androidmkBlocked(rel string, deny, allow []string) bool {
	for _, a := range allow {
		if rel == a {
			return false
		}
	}
	for _, d := range deny {
		if strings.HasPrefix(rel, d) {
			return true
		}
	}
	return false
}

// applyOverlay removes every blocked Android.mk under outRoot/<subtree> and writes the overlay's
// files (overwriting the bundle's copies — the overlay is the conversion of exactly those files).
// Returns the repo-relative makefiles removed and files written; unchanged files are not listed.
func applyOverlay(outRoot string, e overlayEntry, deny, allow []string) (removed, written []string, err error) {
	sub := filepath.Join(outRoot, filepath.FromSlash(e.Subtree))
	walkFiles(sub, func(n string) bool { return n == "Android.mk" }, func(p string) {
		rel, _ := filepath.Rel(outRoot, p)
		rel = filepath.ToSlash(rel)
		if androidmkBlocked(rel, deny, allow) && os.Remove(p) == nil {
			removed = append(removed, rel)
		}
	})
	src := path.Join(overlaysRoot, e.Subtree)
	werr := fs.WalkDir(embeddedOverlays, src, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		rel, rerr := filepath.Rel(filepath.FromSlash(src), filepath.FromSlash(p))
		if rerr != nil {
			return rerr
		}
		b, rerr := embeddedOverlays.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(sub, rel)
		if cur, cerr := os.ReadFile(target); cerr == nil && string(cur) == string(b) {
			return nil
		}
		if merr := os.MkdirAll(filepath.Dir(target), 0o755); merr != nil {
			return merr
		}
		if werr := os.WriteFile(target, b, 0o644); werr != nil {
			return werr
		}
		written = append(written, path.Join(e.Subtree, filepath.ToSlash(rel)))
		return nil
	})
	sort.Strings(removed)
	sort.Strings(written)
	return removed, written, werr
}

// overlayRemovesFile reports whether repo-relative rel (forward-slashed) is a makefile the target's
// denylist blocks AND an embedded overlay converts — i.e. a file the compat pass would delete
// again right after a mirror re-added it. The mirror steps consult this so a re-run over an
// already-treated tree stays quiet instead of re-adding three Android.mk and removing them again.
// The denylist read is cached per outRoot.
var overlayRemovesCache = map[string]func(string) bool{}

func overlayRemovesFile(outRoot, rel string) bool {
	f, ok := overlayRemovesCache[outRoot]
	if !ok {
		deny, allow, err := soongAndroidmkDenylist(outRoot)
		entries := embeddedOverlayEntries()
		if err != nil || len(entries) == 0 {
			f = func(string) bool { return false }
		} else {
			f = func(rel string) bool {
				if path.Base(rel) != "Android.mk" || !androidmkBlocked(rel, deny, allow) {
					return false
				}
				_, has := overlayFor(rel, entries)
				return has
			}
		}
		overlayRemovesCache[outRoot] = f
	}
	return f(rel)
}

// vestigialIncludeOnlyMk reports whether a makefile does nothing but include the makefiles beneath
// it — LOCAL_PATH := $(call my-dir), optional ifeq/ifneq guards, and an
// `include $(call all-makefiles-under|all-subdir-makefiles,…)` line — AND no Android.mk exists
// beneath it (in fsys, rooted at the tree the makefile lives in; dir is its forward-slashed
// directory there), so that including it is a no-op. Such a wrapper survives in a device tree
// after its subdirectory makefiles were converted (Pixel 9a's android-15.0.0_r31 tree; the r36
// families dropped theirs); removing — or never landing — it is the conversion. Line scan, no
// regex (HARD RULE 3).
func vestigialIncludeOnlyMk(content []byte, fsys fs.FS, dir string) bool {
	sawInclude := false
	for _, line := range strings.Split(string(content), "\n") {
		t := strings.TrimSpace(line)
		if i := strings.IndexByte(t, '#'); i >= 0 {
			t = strings.TrimSpace(t[:i])
		}
		switch {
		case t == "":
		case strings.HasPrefix(t, "LOCAL_PATH") && strings.Contains(t, "my-dir"):
		case strings.HasPrefix(t, "ifeq") || strings.HasPrefix(t, "ifneq") || t == "endif" || t == "else":
		case strings.HasPrefix(t, "include") && (strings.Contains(t, "all-makefiles-under") || strings.Contains(t, "all-subdir-makefiles")):
			sawInclude = true
		default:
			return false
		}
	}
	if !sawInclude {
		return false
	}
	found := false
	_ = fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() == "Android.mk" && path.Dir(p) != dir {
			found = true
		}
		return nil
	})
	return !found
}

// mirrorSkipsMakefile reports whether the mirror should never land bundle path rel (forward-slashed,
// repo-relative) from fsys: the target denylists it and either an overlay replaces it or it is a
// vestigial include-only wrapper. Without the skip, every re-run would re-add the file only for
// the compat pass to remove it again.
func mirrorSkipsMakefile(outRoot, rel string, fsys fs.FS) bool {
	if path.Base(rel) != "Android.mk" {
		return false
	}
	if overlayRemovesFile(outRoot, rel) {
		return true
	}
	deny, allow, err := soongAndroidmkDenylist(outRoot)
	if err != nil || !androidmkBlocked(rel, deny, allow) {
		return false
	}
	b, rerr := fs.ReadFile(fsys, rel)
	return rerr == nil && vestigialIncludeOnlyMk(b, fsys, path.Dir(rel))
}

// replaceDenylistedAndroidMk is target-compat operation 4 (see targetcompat.go).
func replaceDenylistedAndroidMk(outRoot string, roots []string, r *compatReport) {
	deny, allow, err := soongAndroidmkDenylist(outRoot)
	if err != nil {
		r.Notes = append(r.Notes, "Android.mk denylist: target has none ("+err.Error()+") — makefiles left as mirrored")
		return
	}
	entries := embeddedOverlayEntries()
	applied := map[string]bool{}
	for _, root := range roots {
		walkFiles(filepath.Join(outRoot, filepath.FromSlash(root)), func(n string) bool { return n == "Android.mk" }, func(p string) {
			rel, _ := filepath.Rel(outRoot, p)
			rel = filepath.ToSlash(rel)
			if !androidmkBlocked(rel, deny, allow) {
				return
			}
			ov, ok := overlayFor(rel, entries)
			if !ok {
				if b, rerr := os.ReadFile(p); rerr == nil && vestigialIncludeOnlyMk(b, os.DirFS(outRoot), path.Dir(rel)) {
					if os.Remove(p) == nil {
						r.MkRemoved = append(r.MkRemoved, rel+" (include-only wrapper with nothing beneath it)")
					}
					return
				}
				r.BlockedMk = append(r.BlockedMk, rel)
				return
			}
			if applied[ov.Subtree] {
				return
			}
			applied[ov.Subtree] = true
			removed, written, aerr := applyOverlay(outRoot, ov, deny, allow)
			if aerr != nil {
				r.Notes = append(r.Notes, "overlay "+ov.Subtree+": "+aerr.Error())
			}
			r.MkRemoved = append(r.MkRemoved, removed...)
			r.OverlayFiles = append(r.OverlayFiles, written...)
			r.Overlays = append(r.Overlays, ov.Subtree+" ← "+ov.Source)
		})
	}
	sort.Strings(r.BlockedMk)
}
