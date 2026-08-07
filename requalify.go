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
	"strings"

	parser "github.com/abstractsrevenge/sovereign_lane_surgeon/internal/blueprint/parser"
)

// requalify.go — §23.1 requalifier. When a lane forks a large subtree (e.g. frameworks/base), the
// finder's keep-name per-file replacement drops each stock parallel bp, which breaks every
// fully-qualified INTERNAL label (`//frameworks/base/X:Mod`) that pointed at the stock path — the
// module now lives at `//frameworks-<lane>/base/X:Mod`. This rewrites those labels to the lane
// path, PRESERVING the `//path:module` form (a repoint, NOT a bareify — sidesteps the §11-12
// bareify SRCS breakage). Fork-boundary aware: a label is only repointed if its target dir was
// actually forked (exists under the lane root) — so unforked subtrees (which fell through to stock)
// keep their stock labels. AST-safe via the vendored blueprint parser (HARD RULE 3 — no regex).

// requalifyLabel repoints a single `//<root>/<path>:<mod>` label to the lane root when the target
// dir is forked. laneMap maps a stock root ("frameworks") to its lane root ("frameworks-<lane>").
// applyRenamePrefix prefixes the app/package segment for the rename model, e.g.
// packages-nexusm/apps/Nfc → packages-nexusm/apps/NexusMNfc (and .../base/packages/<X> →
// .../base/packages/NexusM<X>). Idempotent (skips an already-prefixed segment); leaves paths
// without an apps//base/packages/ marker untouched (so frameworks-nexusm/base/api is unchanged).
func applyRenamePrefix(path, prefix string) string {
	if prefix == "" {
		return path
	}
	for _, m := range []string{"/apps/", "/base/packages/"} {
		i := strings.Index(path, m)
		if i < 0 {
			continue
		}
		rest := path[i+len(m):]
		seg, tail := rest, ""
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			seg, tail = rest[:j], rest[j:]
		}
		if seg == "" || strings.HasPrefix(seg, prefix) {
			return path
		}
		return path[:i+len(m)] + prefix + seg + tail
	}
	return path
}

func requalifyLabel(s, outRoot string, laneMap map[string]string, cache map[string]bool, force bool, prefix string) string {
	if !strings.HasPrefix(s, "//") {
		return s
	}
	body := s[2:]
	pathPart, modPart := body, ""
	if i := strings.IndexByte(body, ':'); i >= 0 {
		pathPart, modPart = body[:i], body[i:]
	}
	for stockRoot, laneRoot := range laneMap {
		if pathPart != stockRoot && !strings.HasPrefix(pathPart, stockRoot+"/") {
			continue
		}
		lanePath := applyRenamePrefix(laneRoot+strings.TrimPrefix(pathPart, stockRoot), prefix)
		if force {
			return "//" + lanePath + modPart // absolute-sovereignty: retarget even if not yet forked
		}
		exists, ok := cache[lanePath]
		if !ok {
			fi, err := os.Stat(filepath.Join(outRoot, lanePath))
			exists = err == nil && fi.IsDir()
			cache[lanePath] = exists
		}
		if exists {
			return "//" + lanePath + modPart
		}
		return s // target not forked — keep the stock label
	}
	return s
}

// requalifyFile rewrites all qualified labels in one bp; returns whether it changed.
func requalifyFile(path, outRoot string, laneMap map[string]string, cache map[string]bool, force bool, prefix string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	file, errs := parser.Parse(path, bytes.NewReader(b))
	if len(errs) > 0 {
		return false, errs[0]
	}
	changed := false
	var walk func(e parser.Expression)
	walk = func(e parser.Expression) {
		switch v := e.(type) {
		case *parser.String:
			if nv := requalifyLabel(v.Value, outRoot, laneMap, cache, force, prefix); nv != v.Value {
				v.Value = nv
				changed = true
			}
		case *parser.List:
			for _, it := range v.Values {
				walk(it)
			}
		case *parser.Map:
			for _, p := range v.Properties {
				walk(p.Value)
			}
		case *parser.Operator:
			walk(v.Args[0])
			walk(v.Args[1])
		}
	}
	for _, def := range file.Defs {
		switch d := def.(type) {
		case *parser.Module:
			for _, p := range d.Properties {
				walk(p.Value)
			}
		case *parser.Assignment:
			walk(d.Value)
		}
	}
	if !changed {
		return false, nil
	}
	out, err := parser.Print(file)
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, out, 0o644)
}

// runRequalify walks the lane's cloned frameworks-<lane>/ + packages-<lane>/ trees and repoints
// every internal qualified label whose target was forked. Skips files that fail to parse (logs).
func runRequalify(c LaneConfig, outRoot string) (changed, failed int) {
	laneMap := map[string]string{
		"frameworks": "frameworks-" + c.Name,
		"packages":   "packages-" + c.Name,
	}
	cache := map[string]bool{}
	roots := []string{"frameworks-" + c.Name, "packages-" + c.Name}
	fmt.Printf("\nrequalify (repoint //<root>/… → //<root>-%s/… for forked targets, AST-safe):\n", c.Name)
	for _, root := range roots {
		rootDir := filepath.Join(outRoot, root)
		if fi, err := os.Stat(rootDir); err != nil || !fi.IsDir() {
			continue
		}
		filepath.Walk(rootDir, func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
				return nil
			}
			ch, ferr := requalifyFile(p, outRoot, laneMap, cache, false, "")
			if ferr != nil {
				failed++
				return nil // resilient: skip unparseable/odd bp, keep going
			}
			if ch {
				changed++
			}
			return nil
		})
	}
	fmt.Printf("  %d bp repointed, %d skipped (parse).\n", changed, failed)
	return changed, failed
}
