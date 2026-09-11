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
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	parser "github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/internal/blueprint/parser"
)

// kotlinSourceParityResult records repairs made by comparing a lane Blueprint with the stock
// Blueprint at the target release. The on-disk target is the version oracle: a lane-added empty
// Kotlin glob is dropped, while a target release that owns the same glob keeps it.
type kotlinSourceParityResult struct {
	Files, Dropped, Repointed int
}

func moduleProperty(m *parser.Module, name string) *parser.Property {
	for _, p := range m.Properties {
		if p.Name == name {
			return p
		}
	}
	return nil
}

func expressionStrings(expr parser.Expression) map[string]bool {
	out := map[string]bool{}
	collectStrings(expr, out)
	return out
}

// editListStrings updates string leaves without flattening list + select expressions. A string is
// removable only when it is a list member; Blueprint source properties use lists in every form the
// scaffold accepts.
func editListStrings(expr parser.Expression, edit func(string) (string, bool)) (dropped, replaced int) {
	switch v := expr.(type) {
	case *parser.List:
		kept := v.Values[:0]
		for _, item := range v.Values {
			if s, ok := item.(*parser.String); ok {
				next, keep := edit(s.Value)
				if !keep {
					dropped++
					continue
				}
				if next != s.Value {
					s.Value = next
					replaced++
				}
			} else {
				d, r := editListStrings(item, edit)
				dropped += d
				replaced += r
			}
			kept = append(kept, item)
		}
		v.Values = kept
	case *parser.Operator:
		d, r := editListStrings(v.Args[0], edit)
		dropped += d
		replaced += r
		d, r = editListStrings(v.Args[1], edit)
		dropped += d
		replaced += r
	case *parser.Select:
		for _, c := range v.Cases {
			if c == nil {
				continue
			}
			d, r := editListStrings(c.Value, edit)
			dropped += d
			replaced += r
		}
		if v.Append != nil {
			d, r := editListStrings(v.Append, edit)
			dropped += d
			replaced += r
		}
	}
	return dropped, replaced
}

func isKotlinSourcePath(s string) bool {
	return !strings.HasPrefix(s, ":") && strings.HasSuffix(strings.ToLower(s), ".kt")
}

// matchPathGlob supports Blueprint's segment form of ** in addition to path.Match's *, ? and [].
func matchPathGlob(pattern, name string) bool {
	pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "./")
	name = strings.TrimPrefix(filepath.ToSlash(name), "./")
	ps, ns := strings.Split(pattern, "/"), strings.Split(name, "/")
	type state struct{ p, n int }
	memo := map[state]bool{}
	seen := map[state]bool{}
	var match func(int, int) bool
	match = func(pi, ni int) bool {
		st := state{pi, ni}
		if seen[st] {
			return memo[st]
		}
		seen[st] = true
		ok := false
		switch {
		case pi == len(ps):
			ok = ni == len(ns)
		case ps[pi] == "**":
			ok = match(pi+1, ni) || ni < len(ns) && match(pi, ni+1)
		case ni < len(ns):
			segment, err := path.Match(ps[pi], ns[ni])
			ok = err == nil && segment && match(pi+1, ni+1)
		}
		memo[st] = ok
		return ok
	}
	return match(0, 0)
}

func sourcePatternMatches(bpDir, pattern string, excluded []string) bool {
	if strings.HasPrefix(pattern, ":") || strings.HasPrefix(pattern, "//") {
		return false
	}
	bpDir = filepath.Clean(bpDir)
	pattern = filepath.ToSlash(pattern)
	effectiveMatch := func(p string) bool {
		rel, err := filepath.Rel(bpDir, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false
		}
		relSlash := filepath.ToSlash(rel)
		if !matchPathGlob(pattern, relSlash) {
			return false
		}
		for _, ex := range excluded {
			if !strings.HasPrefix(ex, ":") && matchPathGlob(ex, relSlash) {
				return false
			}
		}
		return true
	}
	prefix := pattern
	if i := strings.IndexAny(prefix, "*?["); i >= 0 {
		prefix = prefix[:i]
	}
	searchRoot := filepath.Join(bpDir, filepath.FromSlash(strings.TrimSuffix(prefix, "/")))
	if info, err := os.Stat(searchRoot); err == nil && !info.IsDir() {
		return effectiveMatch(searchRoot)
	}
	for {
		info, err := os.Stat(searchRoot)
		if err == nil && info.IsDir() {
			break
		}
		parent := filepath.Dir(searchRoot)
		rel, rerr := filepath.Rel(bpDir, parent)
		if parent == searchRoot || rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false
		}
		searchRoot = parent
	}
	found := false
	_ = filepath.Walk(searchRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || found {
			return nil
		}
		found = effectiveMatch(p)
		return nil
	})
	return found
}

func blueprintModulesByIdentity(content []byte) (map[string]*parser.Module, *parser.File, error) {
	file, errs := parser.Parse("", bytes.NewReader(content))
	if len(errs) > 0 {
		return nil, nil, errs[0]
	}
	modules := map[string]*parser.Module{}
	for _, def := range file.Defs {
		if m, ok := def.(*parser.Module); ok && m.Name() != "" {
			modules[m.Type+"\x00"+m.Name()] = m
		}
	}
	return modules, file, nil
}

func repairKotlinSourceParityBP(lane, stock []byte, bpDir string) ([]byte, int, int, error) {
	laneModules, laneFile, err := blueprintModulesByIdentity(lane)
	if err != nil {
		return lane, 0, 0, err
	}
	stockModules, _, err := blueprintModulesByIdentity(stock)
	if err != nil {
		return lane, 0, 0, err
	}
	dropped, repointed := 0, 0
	for identity, laneModule := range laneModules {
		stockModule := stockModules[identity]
		if stockModule == nil {
			continue // lane-authored module: stock has no semantics to restore
		}
		excludes := []string{}
		if p := moduleProperty(laneModule, "exclude_srcs"); p != nil {
			for s := range expressionStrings(p.Value) {
				excludes = append(excludes, s)
			}
		}
		stockSrcs := map[string]bool{}
		if p := moduleProperty(stockModule, "srcs"); p != nil {
			stockSrcs = expressionStrings(p.Value)
		}
		if p := moduleProperty(laneModule, "srcs"); p != nil {
			d, r := editListStrings(p.Value, func(s string) (string, bool) {
				if !isKotlinSourcePath(s) || stockSrcs[s] || sourcePatternMatches(bpDir, s, excludes) {
					return s, true
				}
				return "", false
			})
			dropped += d
			repointed += r
		}

		stockExcludes := map[string]bool{}
		if p := moduleProperty(stockModule, "exclude_srcs"); p != nil {
			stockExcludes = expressionStrings(p.Value)
		}
		if p := moduleProperty(laneModule, "exclude_srcs"); p != nil {
			d, r := editListStrings(p.Value, func(s string) (string, bool) {
				if strings.Contains(s, "kotlin/") && strings.HasSuffix(s, ".java") {
					candidate := strings.Replace(s, "kotlin/", "java/", 1)
					if stockExcludes[candidate] {
						return candidate, true
					}
				}
				if isKotlinSourcePath(s) && !stockExcludes[s] && !sourcePatternMatches(bpDir, s, nil) {
					return "", false
				}
				return s, true
			})
			dropped += d
			repointed += r
		}
	}
	if dropped == 0 && repointed == 0 {
		return lane, 0, 0, nil
	}
	out, err := parser.Print(laneFile)
	return out, dropped, repointed, err
}

func stockParallelBP(c LaneConfig, outRoot, lanePath string) string {
	for _, root := range []string{"frameworks", "packages"} {
		laneRoot := filepath.Join(outRoot, root+"-"+c.Name)
		rel, err := filepath.Rel(laneRoot, lanePath)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if c.ParentDirSuffix != "" {
			parent := -1
			if root == "frameworks" && len(parts) > 2 && parts[0] == "base" && parts[1] == "packages" {
				parent = 2
			} else if root == "packages" && len(parts) > 1 && parts[0] == "apps" {
				parent = 1
			}
			if parent >= 0 && strings.HasSuffix(parts[parent], c.ParentDirSuffix) {
				parts[parent] = strings.TrimSuffix(parts[parent], c.ParentDirSuffix)
			}
		}
		return filepath.Join(outRoot, root, filepath.FromSlash(strings.Join(parts, "/")))
	}
	return ""
}

// runRepairInheritedKotlinSourceDrift compares the new lane with stock from the target checkout.
// It removes only lane-added Kotlin paths that resolve to no effective source and restores a
// java/ exclusion when an inherited transformation changed that exact stock entry to kotlin/.
// Valid lane-authored Kotlin and source patterns owned by the target release are retained.
func runRepairInheritedKotlinSourceDrift(c LaneConfig, outRoot string) kotlinSourceParityResult {
	return runRepairInheritedKotlinSourceDriftIn(c, outRoot, []string{
		filepath.Join(outRoot, "frameworks-"+c.Name),
		filepath.Join(outRoot, "packages-"+c.Name),
	})
}

func runRepairInheritedKotlinSourceDriftIn(c LaneConfig, outRoot string, laneRoots []string) kotlinSourceParityResult {
	result := kotlinSourceParityResult{}
	for _, laneRoot := range laneRoots {
		_ = filepath.Walk(laneRoot, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || filepath.Base(p) != "Android.bp" {
				return nil
			}
			stockPath := stockParallelBP(c, outRoot, p)
			stock, serr := os.ReadFile(stockPath)
			lane, lerr := os.ReadFile(p)
			if serr != nil || lerr != nil {
				return nil
			}
			updated, dropped, repointed, rerr := repairKotlinSourceParityBP(lane, stock, filepath.Dir(p))
			if rerr != nil || dropped+repointed == 0 {
				return nil
			}
			if werr := os.WriteFile(p, updated, info.Mode().Perm()); werr != nil {
				return nil
			}
			result.Files++
			result.Dropped += dropped
			result.Repointed += repointed
			return nil
		})
	}
	if result.Files > 0 {
		fmt.Printf("\nKotlin source parity: repaired %d Blueprint file(s): %d empty inherited path(s) removed, %d stock java/ exclusion(s) restored.\n",
			result.Files, result.Dropped, result.Repointed)
	}
	return result
}
