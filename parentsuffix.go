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
	"path/filepath"
	"sort"
	"strings"
)

type parentPathRename struct {
	from string
	to   string
}

func parentSuffixRenames(c LaneConfig, outRoot string) []parentPathRename {
	if c.ParentDirSuffix == "" {
		return nil
	}
	var out []parentPathRename
	for _, base := range []string{
		filepath.Join("frameworks-"+c.Name, "base", "packages"),
		filepath.Join("packages-"+c.Name, "apps"),
	} {
		entries, err := os.ReadDir(filepath.Join(outRoot, base))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasSuffix(entry.Name(), c.ParentDirSuffix) {
				continue
			}
			plain := strings.TrimSuffix(entry.Name(), c.ParentDirSuffix)
			out = append(out, parentPathRename{
				from: filepath.ToSlash(filepath.Join(base, plain)),
				to:   filepath.ToSlash(filepath.Join(base, entry.Name())),
			})
			if c.FromLane != "" {
				sourceBase := strings.Replace(base, "-"+c.Name, "-"+c.FromLane, 1)
				out = append(out, parentPathRename{
					from: filepath.ToSlash(filepath.Join(sourceBase, plain)),
					to:   filepath.ToSlash(filepath.Join(base, entry.Name())),
				})
			}
		}
	}
	// Longest first makes the audit deterministic and avoids needless partial scans for names such
	// as Settings and SettingsLib. replaceBoundedPath also enforces the segment boundary.
	sort.Slice(out, func(i, j int) bool { return len(out[i].from) > len(out[j].from) })
	return out
}

func isPathBoundary(b byte) bool {
	switch b {
	case '/', ':', '"', '\'', ' ', '\t', '\r', '\n', ')', ']', '}', ',', ';', '\\':
		return true
	default:
		return false
	}
}

// replaceBoundedPath rewrites a rooted path only when the matched parent is a complete segment.
// This prevents Settings from corrupting SettingsLib while still covering Blueprint labels,
// include paths, make variables, and paths embedded in command strings.
func replaceBoundedPath(src []byte, from, to string) ([]byte, int) {
	needle := []byte(from)
	repl := []byte(to)
	var out []byte
	count := 0
	for {
		i := bytes.Index(src, needle)
		if i < 0 {
			out = append(out, src...)
			break
		}
		end := i + len(needle)
		out = append(out, src[:end]...)
		if end == len(src) || isPathBoundary(src[end]) {
			out = out[:len(out)-len(needle)]
			out = append(out, repl...)
			count++
		}
		src = src[end:]
	}
	return out, count
}

// runRewriteParentSuffixPaths updates rooted references after the physical mirror has renamed the
// immediate app/package parents. Descendant paths and module names are untouched.
func runRewriteParentSuffixPaths(c LaneConfig, outRoot string) (files, references int) {
	renames := parentSuffixRenames(c, outRoot)
	if len(renames) == 0 {
		return 0, 0
	}
	wanted := map[string]bool{".bp": true, ".bzl": true, ".bazel": true}
	for _, ext := range laneSourcePathExts {
		wanted[ext] = true
	}
	for _, root := range []string{"frameworks-" + c.Name, "packages-" + c.Name} {
		filepath.Walk(filepath.Join(outRoot, root), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || info.Size() > 8<<20 || !wanted[strings.ToLower(filepath.Ext(path))] {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			updated := content
			fileRefs := 0
			for _, rename := range renames {
				var n int
				updated, n = replaceBoundedPath(updated, rename.from, rename.to)
				fileRefs += n
			}
			// Blueprint path properties may be relative to the declaring package rather than
			// repository-rooted. Derive their exact old/new spelling from the same approved parent
			// map. Example: frameworks-holo2/base/Android.bp uses packages/Vcn/... for the
			// frameworks/base/packages/Vcn child, which physically became Vcn_holo2.
			if strings.EqualFold(filepath.Ext(path), ".bp") {
				fileDir, relErr := filepath.Rel(outRoot, filepath.Dir(path))
				if relErr == nil {
					for _, rename := range renames {
						if !strings.HasPrefix(rename.from, root+"/") {
							continue // source-lane rooted maps are never relative to the new lane file
						}
						fromRel, fromErr := filepath.Rel(fileDir, filepath.FromSlash(rename.from))
						toRel, toErr := filepath.Rel(fileDir, filepath.FromSlash(rename.to))
						if fromErr != nil || toErr != nil || fromRel == "." || fromRel == toRel {
							continue
						}
						var n int
						updated, n = replaceBoundedPath(updated, filepath.ToSlash(fromRel), filepath.ToSlash(toRel))
						fileRefs += n
					}
				}
			}
			if fileRefs > 0 && os.WriteFile(path, updated, info.Mode().Perm()) == nil {
				files++
				references += fileRefs
			}
			return nil
		})
	}
	fmt.Printf("\nparent-directory suffix paths (%s): %d reference(s) updated in %d file(s).\n", c.ParentDirSuffix, references, files)
	return files, references
}
