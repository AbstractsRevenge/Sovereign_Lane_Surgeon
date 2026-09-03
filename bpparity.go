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
	"io/fs"
	"os"
	"path/filepath"

	parser "github.com/AbstractsRevenge/sovereign_lane_surgeon/internal/blueprint/parser"
)

// bpparity.go — structural parity check: for every Android.bp under a mirrored/seeded subtree that
// ALREADY existed in the target (so mirrorStockTree/copyDeviceFamilyTree/preSeedFromEmbedded's
// no-clobber rule left it alone), compare its module set against the reference source's copy of
// the same file. Flags any module present in the reference but missing from the target copy — a
// PROACTIVE, pre-build check that catches exactly the "fabricated/incomplete stub" bug class this
// project hit reviving Panther/Cheetah (hardware/google/gchips/Android.bp etc. replaced with
// plausible-looking stubs missing the real modules) BEFORE a build, which error-text
// classification can't do (that bug produced no error where it was introduced — see the KNOWN GAP
// note in taxonomy.go). Deliberately does NOT attempt full dependency-graph reachability — that's
// Soong's own job (`m nothing`, fed through audit.go's Classify()), not something to
// half-reimplement here (a hand-rolled walker would get namespaces/variants/selects/defaults
// subtly wrong compared to the real thing).

// bpModule is one parsed module's identity (name + Soong module type) from a single Android.bp.
type bpModule struct {
	Name string
	Type string
}

// parseBPModules parses content as an Android.bp and returns its top-level modules (Assignments
// are not modules and are skipped). A parse failure returns a non-nil error — callers must treat
// that as "can't compare", not "zero modules".
func parseBPModules(content []byte) ([]bpModule, error) {
	file, errs := parser.Parse("", bytes.NewReader(content))
	if len(errs) > 0 {
		return nil, errs[0]
	}
	var mods []bpModule
	for _, def := range file.Defs {
		m, ok := def.(*parser.Module)
		if !ok {
			continue
		}
		mods = append(mods, bpModule{Name: m.Name(), Type: m.Type})
	}
	return mods, nil
}

// ParityFinding is one "the reference has a module the target's existing copy of the same file
// doesn't" result.
type ParityFinding struct {
	RelPath     string
	MissingName string
	MissingType string
}

// checkBPParity compares one Android.bp's module set between the reference source and the target
// (both already read as bytes by the caller) and returns any modules present in source but absent
// from target. An unparseable REFERENCE is treated as "nothing to say" (can't trust the
// comparison); an unparseable TARGET is itself reported as a finding (a target .bp that fails to
// parse is a real, worth-surfacing problem on its own).
func checkBPParity(relPath string, sourceContent, targetContent []byte) []ParityFinding {
	srcMods, srcErr := parseBPModules(sourceContent)
	if srcErr != nil {
		return nil
	}
	tgtMods, tgtErr := parseBPModules(targetContent)
	if tgtErr != nil {
		return []ParityFinding{{RelPath: relPath, MissingName: "(entire file)", MissingType: "PARSE ERROR: " + tgtErr.Error()}}
	}
	tgtNames := make(map[string]bool, len(tgtMods))
	for _, m := range tgtMods {
		tgtNames[m.Name] = true
	}
	var findings []ParityFinding
	for _, m := range srcMods {
		if m.Name == "" {
			continue // unnamed defs (namespace decls, etc.) aren't module-identity comparable
		}
		if !tgtNames[m.Name] {
			findings = append(findings, ParityFinding{RelPath: relPath, MissingName: m.Name, MissingType: m.Type})
		}
	}
	return findings
}

// checkBPParityForTree walks every Android.bp reachable from sourceFS at relRoot and, for each one
// that ALSO exists at outRoot/<relRoot>/... (a fresh mirror is byte-identical by construction, so
// parity is only in question for a file that already existed and was left alone), runs
// checkBPParity. relRoot must be forward-slashed (fs.FS convention).
func checkBPParityForTree(sourceFS fs.FS, relRoot, outRoot string) []ParityFinding {
	var findings []ParityFinding
	_ = fs.WalkDir(sourceFS, relRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "Android.bp" {
			return nil
		}
		targetPath := filepath.Join(outRoot, filepath.FromSlash(p))
		targetContent, terr := os.ReadFile(targetPath)
		if terr != nil {
			return nil // not present in target at all — nothing to compare
		}
		sourceContent, serr := fs.ReadFile(sourceFS, p)
		if serr != nil {
			return nil
		}
		findings = append(findings, checkBPParity(p, sourceContent, targetContent)...)
		return nil
	})
	return findings
}

// printParityFindings reports findings in the same module|recipe shape audit.go's cmdAudit uses,
// so this composes with the existing taxonomy output rather than inventing a new report format.
func printParityFindings(findings []ParityFinding) {
	if len(findings) == 0 {
		return
	}
	fmt.Printf("  ! bp-parity: %d module(s) present in the reference source but missing from the target's existing copy (pre-existing file, not overwritten — likely stale/incomplete):\n", len(findings))
	for _, f := range findings {
		fmt.Printf("      %s: missing %q (%s)\n", f.RelPath, f.MissingName, f.MissingType)
	}
}

// bestSourceFS resolves the fs.FS a mirrorFromBestSource "source" label (see devicerevival.go)
// refers to, for feeding into checkBPParityForTree.
func bestSourceFS(c LaneConfig, source string) fs.FS {
	if source == "source-root" && c.SourceRoot != "" {
		return os.DirFS(c.SourceRoot)
	}
	return embeddedFS
}
