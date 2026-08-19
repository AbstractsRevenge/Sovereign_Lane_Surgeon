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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// soongstage.go — the preview-then-apply model for the in-place soong patches (T-directed
// 2026-07-12: a published tool editing a user's AOSP soong must let them review before it
// commits). `create -out` STAGES the patched build/soong/*.go files under <out>/.sld-staging/
// and prints a focused diff; it NEVER touches the live files. A separate `apply -out <tree>`
// step snapshots each live file (.sld-snapshots/<ts>/) then commits the staged version.

const (
	stageDir    = ".sld-staging"
	snapshotDir = ".sld-snapshots"
)

// soongTarget is one in-place-patched soong file.
type soongTarget struct {
	rel     string
	patched []byte
	changed bool
}

// stageSoongPatches computes the aar.go / visibility.go / finder.go patches for the lane,
// writes the patched results under <outRoot>/.sld-staging/, and prints a diff per changed file.
// Reads the live files but does not modify them.
func stageSoongPatches(c LaneConfig, outRoot string) (staged int, fatal bool) {
	aarRel := filepath.Join("build", "soong", "java", "aar.go")
	visRel := filepath.Join("build", "soong", "android", "visibility.go")
	finRel := filepath.Join("build", "soong", "ui", "build", "finder.go")

	// Chain-aware: read the STAGED file if a prior lane in this batch already patched it, else
	// the live tree — so multi-lane `create` accumulates patches (each lane blinds the others).
	aarSrc, err := readStagedOrLive(outRoot, aarRel)
	if err != nil {
		fmt.Printf("\nsoong patches: skipped (%s not found under -out — is this an AOSP root?)\n", aarRel)
		return 0, false
	}
	visSrc, err := readStagedOrLive(outRoot, visRel)
	if err != nil {
		fmt.Printf("\nsoong patches: skipped (%s not found)\n", visRel)
		return 0, false
	}
	finSrc, err := readStagedOrLive(outRoot, finRel)
	if err != nil {
		fmt.Printf("\nsoong patches: skipped (%s not found)\n", finRel)
		return 0, false
	}

	targets := make([]soongTarget, 0, 3)

	aarOut, aChanged, err := PatchIsLaneLunch(aarSrc, c.Name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! patch aar.go: %v\n", err)
		return 0, true
	}
	// Framework-class is KEEP-NAME in BOTH models now (Model-A hybrid — see runRenameFrameworkClass):
	// framework-res keeps the name "framework-res", so the renamed-name special-casing (appendFrameworkResCase,
	// which added "framework-res-<lane>" to isFrameworkResClassName) is no longer needed. The keep-name
	// framework-res is handled by the isLaneLunch-gated shouldSuppressStockFrameworkRes suppressor that
	// PatchIsLaneLunch (above) already enables. (appendFrameworkResCase remains defined + unit-tested.)
	targets = append(targets, soongTarget{aarRel, aarOut, aChanged})

	visOut, vChanged, err := PatchLaneCanonicalPkgs(visSrc, c.Name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! patch visibility.go: %v\n", err)
		return 0, true
	}
	targets = append(targets, soongTarget{visRel, visOut, vChanged})

	// neverallow.go: allowlist the lane's ravenwood/layoutlib paths. Only relevant when the lane
	// forks frameworks/base, but harmless (and forward-proof) otherwise — the entries simply go
	// unused. Soft-skipped if the file or the list is absent on this AOSP version.
	nevRel := filepath.Join("build", "soong", "android", "neverallow.go")
	var nevSrc []byte
	nevStaged := false
	if ns, nerr := readStagedOrLive(outRoot, nevRel); nerr == nil {
		nevSrc = ns
		nevOut, nChanged, perr := PatchNeverallowLanePaths(nevSrc, c.Name)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "  ! patch neverallow.go: %v\n", perr)
			return 0, true
		}
		targets = append(targets, soongTarget{nevRel, nevOut, nChanged})
		nevStaged = true
	}

	suffixes, err := deriveOtherLaneSuffixes(aarSrc, c.Name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! derive other-lane suffixes: %v\n", err)
		return 0, true
	}
	block, err := genFinderLaneFuncs(c, suffixes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! generate finder funcs: %v\n", err)
		return 0, true
	}
	finOut, f1, err := insertLaneFuncs(finSrc, c.CamelCase, block)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! insert finder funcs: %v\n", err)
		return 0, true
	}
	finOut, f2, err := insertPipelineCall(finOut, c.CamelCase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! insert pipeline call: %v\n", err)
		return 0, true
	}
	finOut, _, err = patchExistingOtherLaneFuncs(finOut, c.Name, c.CamelCase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! cross-cut other-lane funcs: %v\n", err)
		return 0, true
	}
	// MUST follow the cross-cut: that step is what makes isOtherLaneBp match this lane's suffix,
	// which is what would otherwise make the lane drop its own bp before its routes run.
	// NOTE: on error this returns a nil slice, so it must not be assigned back over finOut —
	// doing so discards every finder patch accumulated above.
	guardOut, f3, err := excludeLaneFromStockDrop(finOut, c.CamelCase)
	if err != nil {
		// Soft-skip: a finder with no dropNonHoloLaneBps guard has nothing to exclude the lane
		// from, so this is a genuine no-op rather than a failure. Noted, not silent — if the guard
		// exists under another name, the lane would drop its own bp and build as pure stock.
		fmt.Printf("  ~ %s: no stock-drop guard found — skipped (%v)\n", finRel, err)
		f3 = false
	} else {
		finOut = guardOut
	}
	targets = append(targets, soongTarget{finRel, finOut, f1 || f2 || f3 || !bytesEqual(finOut, finSrc)})

	// Lane-INDEPENDENT stock fix (T-directed 2026-07-13): license-metadata back-fill so a
	// full-image / compatibility-suite build doesn't trip "<tool> has no license metadata".
	// Chain-aware read → idempotent across a multi-lane batch. Skips cleanly on AOSP-version
	// drift (block not in the known stock form) rather than guessing at a brace-heavy make loop.
	compatRel := filepath.Join("build", "make", "core", "tasks", "tools", "compatibility.mk")
	var compatSrc []byte
	compatStaged := false
	if cs, cerr := readStagedOrLive(outRoot, compatRel); cerr == nil {
		compatSrc = cs
		compatOut, compatCh, compatFound, perr := patchCompatibilityMk(compatSrc)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "  ! patch compatibility.mk: %v\n", perr)
			return 0, true
		}
		if !compatFound {
			fmt.Printf("  ~ %s: license-metadata block not in known stock form — skipped (patch by hand if you build compatibility suites)\n", compatRel)
		} else {
			targets = append(targets, soongTarget{compatRel, compatOut, compatCh})
			compatStaged = true
		}
	}

	stageRoot := filepath.Join(outRoot, stageDir)
	origs := map[string][]byte{aarRel: aarSrc, visRel: visSrc, finRel: finSrc}
	if compatStaged {
		origs[compatRel] = compatSrc
	}
	if nevStaged {
		origs[nevRel] = nevSrc
	}
	fmt.Printf("\nsoong lane-routing patches (STAGED — review, then `sovereign-lane-surgeon apply -out %s`):\n", outRoot)
	for _, t := range targets {
		if !t.changed {
			fmt.Printf("  = %s already registers %q — no change\n", t.rel, c.Name)
			continue
		}
		stagePath := filepath.Join(stageRoot, t.rel)
		if err := os.MkdirAll(filepath.Dir(stagePath), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "  ! mkdir stage: %v\n", err)
			return staged, true
		}
		if err := os.WriteFile(stagePath, t.patched, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "  ! write stage %s: %v\n", t.rel, err)
			return staged, true
		}
		fmt.Printf("\n%s", previewDiff(t.rel, origs[t.rel], t.patched))
		staged++
	}
	return staged, false
}

// applyStaged commits every file under <outRoot>/.sld-staging/ onto the live tree, snapshotting
// each live file to .sld-snapshots/<ts>/ first (Rule 7 — the tree is not git). Clears staging.
func applyStaged(outRoot string) int {
	stageRoot := filepath.Join(outRoot, stageDir)
	var rels []string
	err := filepath.Walk(stageRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, rerr := filepath.Rel(stageRoot, p)
			if rerr != nil {
				return rerr
			}
			rels = append(rels, rel)
		}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("nothing staged (%s absent) — run `create -out %s` first\n", stageDir, outRoot)
			return 0
		}
		fmt.Fprintln(os.Stderr, "apply:", err)
		return 1
	}
	if len(rels) == 0 {
		fmt.Println("nothing staged.")
		return 0
	}
	ts := time.Now().UTC().Format("20060102T150405Z")
	snapRoot := filepath.Join(outRoot, snapshotDir, ts)
	for _, rel := range rels {
		live := filepath.Join(outRoot, rel)
		if _, serr := os.Stat(live); serr == nil {
			snap := filepath.Join(snapRoot, rel)
			if cerr := copyFile(live, snap); cerr != nil {
				fmt.Fprintf(os.Stderr, "apply: snapshot %s failed: %v (aborting before any write)\n", rel, cerr)
				return 1
			}
		}
		if cerr := copyFile(filepath.Join(stageRoot, rel), live); cerr != nil {
			fmt.Fprintf(os.Stderr, "apply: commit %s failed: %v\n", rel, cerr)
			return 1
		}
		fmt.Printf("  ✓ %s (snapshot → %s/%s)\n", rel, snapshotDir+"/"+ts, rel)
	}
	if err := os.RemoveAll(stageRoot); err != nil {
		fmt.Fprintf(os.Stderr, "apply: could not clear staging: %v\n", err)
	}
	fmt.Printf("\napplied %d file(s). Snapshots in %s/%s. Next: `aosp_build_capture -lane <lane> -build-cmd 'm -jN nothing'` to soong-verify.\n", len(rels), snapshotDir, ts)
	return 0
}

// previewDiff renders a readable multi-hunk line diff (LCS-based, 3 lines of context, "..."
// between non-contiguous changes) — correct for the several separated insertions the finder
// patch makes, not just a single region.
func previewDiff(path string, old, patched []byte) string {
	if string(old) == string(patched) {
		return ""
	}
	a := strings.Split(string(old), "\n")
	b := strings.Split(string(patched), "\n")
	n, m := len(a), len(b)
	// LCS length table (bottom-up).
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	type op struct {
		k byte
		s string
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{' ', a[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, op{'-', a[i]})
			i++
		default:
			ops = append(ops, op{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{'+', b[j]})
	}
	const ctx = 3
	show := make([]bool, len(ops))
	for idx, o := range ops {
		if o.k != ' ' {
			for k := idx - ctx; k <= idx+ctx; k++ {
				if k >= 0 && k < len(ops) {
					show[k] = true
				}
			}
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- a/%s\n+++ b/%s\n", path, path)
	prevShown := false
	for idx, o := range ops {
		if show[idx] {
			if !prevShown && idx > 0 {
				out.WriteString("  ...\n")
			}
			fmt.Fprintf(&out, "%c%s\n", o.k, o.s)
			prevShown = true
		} else {
			prevShown = false
		}
	}
	return out.String()
}

// readStagedOrLive returns the staged copy of rel (from a prior lane in this batch) if present,
// else the live-tree file — the accumulation mechanism for multi-lane create.
func readStagedOrLive(outRoot, rel string) ([]byte, error) {
	if b, err := os.ReadFile(filepath.Join(outRoot, stageDir, rel)); err == nil {
		return b, nil
	}
	return os.ReadFile(filepath.Join(outRoot, rel))
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func bytesEqual(a, b []byte) bool { return string(a) == string(b) }
