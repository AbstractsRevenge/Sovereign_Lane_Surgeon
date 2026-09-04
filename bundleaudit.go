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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// bundleaudit.go — `bundle audit -source <aosp tree>`: prove the bundle is a faithful copy of the
// tree it was cut from, across every property `go:embed` can lose.
//
// WHY: embed.FS carries regular-file CONTENT and nothing else. Two losses were found the
// expensive way — the executable bit (tangorpro, kati "Permission denied") and symlinks (husky,
// "file not found" 46 minutes into a build) — each patched with its own manifest. Patching
// instances does not close the class: nothing said what ELSE was missing, and the answer arrived
// as a failed build. This walks source and bundle together and reports every divergence by
// class, so the question is answered by measurement instead of by the next outage.
//
// Excluded by design (declared here, never silent):
//   - `.git` — in a repo-managed tree this is a gitfile pointing into .repo, meaningless elsewhere
//   - `<family>-kernels/` — ~2GB of prebuilt kernels, and the wrong build for a newer release
//     anyway; the matching kernel comes from the factory image (kernelprebuilt.go)

// bundleExclusion is a path the bundle deliberately does not carry.
type bundleExclusion struct {
	Match  func(rel string, d fs.DirEntry) bool
	Reason string
}

var bundleExclusions = []bundleExclusion{
	{
		Match:  func(rel string, d fs.DirEntry) bool { return filepath.Base(rel) == ".git" },
		Reason: ".git gitfile/dir — points into .repo, meaningless outside the source checkout",
	},
	{
		Match: func(rel string, d fs.DirEntry) bool {
			for _, seg := range strings.Split(rel, "/") {
				if strings.HasSuffix(seg, "-kernels") {
					return true
				}
			}
			return false
		},
		Reason: "<family>-kernels/ — prebuilt kernels, excluded on purpose (assembled from the factory image instead)",
	},
}

func excludedFromBundle(rel string, d fs.DirEntry) (bool, string) {
	for _, e := range bundleExclusions {
		if e.Match(rel, d) {
			return true, e.Reason
		}
	}
	return false, ""
}

// auditFinding is one divergence between a source tree and the bundle.
type auditFinding struct {
	Class string // missing-file, content, exec-bit, symlink, symlink-target, empty-dir, unsupported, extra
	Path  string
	Note  string
}

// bundleAuditResult is the whole comparison for one source tree.
type bundleAuditResult struct {
	SourceRoot string
	Audited    []string // top-level bundle dirs this source covers
	Checked    int      // entries compared
	Excluded   int
	Findings   []auditFinding
}

func fileSHA256(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// bundleContentIndex maps bundle path → manifest entry.
func bundleContentIndex() map[string]bundleEntry {
	idx := make(map[string]bundleEntry, len(bundleEntries))
	for _, e := range bundleEntries {
		idx[e.Path] = e
	}
	return idx
}

// auditBundleAgainstSource compares every bundle top-level directory that exists under srcRoot
// with the bundle's own manifests. Only directories present in srcRoot are audited: the bundle
// spans two AOSP tags, so a caller passes each source tree and every directory is covered by
// exactly one of them.
func auditBundleAgainstSource(srcRoot string, topDirs []string) (*bundleAuditResult, error) {
	res := &bundleAuditResult{SourceRoot: srcRoot}
	content := bundleContentIndex()
	execSet := embeddedExecutables
	linkTargets := map[string]string{}
	for _, l := range bundleSymlinks {
		linkTargets[l.Path] = l.Target
	}
	emptyDirs := map[string]bool{}
	for _, d := range bundleEmptyDirs {
		emptyDirs[d] = true
	}
	seen := map[string]bool{}

	for _, top := range topDirs {
		srcTop := filepath.Join(srcRoot, filepath.FromSlash(top))
		info, err := os.Stat(srcTop)
		if err != nil || !info.IsDir() {
			continue // this source tree does not carry that directory
		}
		res.Audited = append(res.Audited, top)
		walkErr := filepath.WalkDir(srcTop, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			relOS, rerr := filepath.Rel(srcRoot, p)
			if rerr != nil {
				return rerr
			}
			rel := filepath.ToSlash(relOS)
			if rel == top {
				return nil
			}
			if ok, _ := excludedFromBundle(rel, d); ok {
				res.Excluded++
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			res.Checked++
			switch {
			case d.IsDir():
				empty, derr := dirHoldsNoFiles(p)
				if derr != nil {
					return derr
				}
				if empty && !emptyDirs[rel] {
					res.Findings = append(res.Findings, auditFinding{"empty-dir", rel,
						"source has an empty directory the bundle does not record (embed drops it)"})
				}
				return nil
			case d.Type()&fs.ModeSymlink != 0:
				seen[rel] = true
				target, lerr := os.Readlink(p)
				if lerr != nil {
					return lerr
				}
				want, have := filepath.ToSlash(target), linkTargets[rel]
				if have == "" {
					res.Findings = append(res.Findings, auditFinding{"symlink", rel,
						"source has a symlink -> " + want + "; the bundle does not carry it (embed drops symlinks)"})
				} else if have != want {
					res.Findings = append(res.Findings, auditFinding{"symlink-target", rel,
						"source -> " + want + ", bundle -> " + have})
				}
				return nil
			case !d.Type().IsRegular():
				res.Findings = append(res.Findings, auditFinding{"unsupported", rel,
					"source entry is " + d.Type().String() + "; the bundle cannot represent it"})
				return nil
			}
			seen[rel] = true
			entry, ok := content[rel]
			if !ok {
				res.Findings = append(res.Findings, auditFinding{"missing-file", rel, "in the source, absent from the bundle"})
				return nil
			}
			sum, size, herr := fileSHA256(p)
			if herr != nil {
				return herr
			}
			if size != entry.Size || sum != entry.SHA256 {
				res.Findings = append(res.Findings, auditFinding{"content", rel,
					fmt.Sprintf("source %s/%d bytes, bundle %s/%d bytes", sum[:12], size, entry.SHA256[:12], entry.Size)})
			}
			fi, serr := d.Info()
			if serr != nil {
				return serr
			}
			srcExec := fi.Mode()&0o111 != 0
			if srcExec != execSet[rel] {
				state := map[bool]string{true: "executable", false: "not executable"}
				res.Findings = append(res.Findings, auditFinding{"exec-bit", rel,
					"source is " + state[srcExec] + ", bundle records " + state[execSet[rel]]})
			}
			return nil
		})
		if walkErr != nil {
			return res, walkErr
		}
	}

	// Anything the bundle carries under an audited directory that the source no longer has.
	for _, top := range res.Audited {
		for _, e := range bundleEntries {
			if (e.Path == top || strings.HasPrefix(e.Path, top+"/")) && !seen[e.Path] {
				res.Findings = append(res.Findings, auditFinding{"extra", e.Path, "in the bundle, absent from this source tree"})
			}
		}
		for _, l := range bundleSymlinks {
			if (l.Path == top || strings.HasPrefix(l.Path, top+"/")) && !seen[l.Path] {
				res.Findings = append(res.Findings, auditFinding{"extra", l.Path, "symlink in the bundle, absent from this source tree"})
			}
		}
	}
	sort.Slice(res.Findings, func(i, j int) bool {
		if res.Findings[i].Class != res.Findings[j].Class {
			return res.Findings[i].Class < res.Findings[j].Class
		}
		return res.Findings[i].Path < res.Findings[j].Path
	})
	return res, nil
}

// dirHoldsNoFiles reports whether a directory contains no file, link or non-empty subdirectory.
func dirHoldsNoFiles(dir string) (bool, error) {
	empty := true
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if p == dir || d.IsDir() {
			return nil
		}
		if ok, _ := excludedFromBundle(filepath.ToSlash(p), d); ok {
			return nil
		}
		empty = false
		return filepath.SkipAll
	})
	return empty, err
}

// bundleTopDirs lists the bundle's top-level directories ("device/google/lynx"), the unit
// provenance is recorded in and the unit a source tree either carries or does not.
func bundleTopDirs() []string {
	set := map[string]bool{}
	for _, e := range bundleEntries {
		if f := strings.SplitN(e.Path, "/", 4); len(f) >= 3 {
			set[f[0]+"/"+f[1]+"/"+f[2]] = true
		}
	}
	var out []string
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
