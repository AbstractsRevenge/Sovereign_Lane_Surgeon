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

// allowdeps.go — "a lane owns its allowed-deps delta" (lane-sovereign apex allow-listing).
//
// WHY THIS EXISTS. The apex-allowed-deps check (build/soong/apex/apex_singleton.go)
// validates every SOURCE-built updatable apex's dependency set against an allow-list.
// It reads that list from a RAW SOURCE PATH — literally
// "packages/modules/common/build/allowed_deps.txt" via ExistentPathForSource — not a
// module reference. The lane finder only routes Android.bp *module* resolution, so it
// CANNOT redirect that read: a lane's forked updatable apexes (com.android.permission,
// com.android.mediaprovider, ...) would otherwise be gated by, and force edits into, the
// stock file — breaking lane sovereignty (mutual invisibility) on this one surface.
//
// The fix Soong already gives us: EXTRA_ALLOWED_DEPS_TXT (build/make/core/soong_config.mk
// -> ExtraAllowedDepsTxt), a second source apex_singleton APPENDS and UNIONS before the
// diff. So a lane can own ONLY its own delta — the deps its forked apexes add beyond
// pristine upstream — in packages-<lane>/modules/common/build/allowed_deps.txt, wired by
// a sibling allowed_deps.mk that each lane device product inherits. Stock stays pristine
// and upstream-tracking; no other lane can see the lane's entries. (deviceproduct.go's
// product template pre-wires the inherit-product-if-exists line for lanes created by
// `create`; this command retrofits an existing lane and regenerates the delta.)
//
// THE DELTA IS DERIVED, NOT HAND-MAINTAINED. A build writes the exact set it computed to
// <out>/soong/apex/depsinfo/new-allowed-deps.txt. The lane delta is precisely
//     computed_entries  MINUS  pristine_stock_entries
// (comment-stripped, sorted-unique). This command computes that and regenerates the lane
// file — which also makes it the DRIFT-GUARD: re-run after any lane apex change and it
// reports entries added/removed, catching a newly-introduced lane dep before it reddens
// a check. It never modifies stock.

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// cmdAllowedDeps is the `allowed-deps` subcommand entry point.
func cmdAllowedDeps(args []string) int {
	fs := flag.NewFlagSet("allowed-deps", flag.ExitOnError)
	root := fs.String("out", "", "AOSP root the lane is adopted into (required)")
	name := fs.String("name", "holo", "lane name (derives packages-<lane>, device/*/*-<lane>, ...)")
	computed := fs.String("computed", "", "path to a build's new-allowed-deps.txt (default: newest under <out>/out*)")
	stock := fs.String("stock", "", "path to pristine stock allowed_deps.txt (default: <out>/packages/modules/common/build/allowed_deps.txt)")
	apply := fs.Bool("apply", false, "commit the wiring (default: preview only)")
	_ = fs.Parse(args)
	if *root == "" {
		fmt.Fprintln(os.Stderr, "allowed-deps: -out <aosp-root> is required")
		return 2
	}
	c := deriveLane(*name, true, nil, false, false, "")
	return runAllowedDeps(c, *root, *computed, *stock, *apply)
}

// runAllowedDeps derives the lane's allowed-deps delta and wires the lane-sovereign
// EXTRA_ALLOWED_DEPS_TXT. Preview by default; apply commits (snapshotting each edited
// product .mk first, Rule 7). Returns a process exit code.
func runAllowedDeps(c LaneConfig, root, computedPath, stockPath string, apply bool) int {
	laneDir := "packages" + c.DirSuffix // packages-holo
	if stockPath == "" {
		stockPath = filepath.Join(root, "packages", "modules", "common", "build", "allowed_deps.txt")
	}

	stockSet, err := readAllowedDepsEntries(stockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "allowed-deps: read stock %s: %v\n", stockPath, err)
		return 1
	}

	if computedPath == "" {
		computedPath = newestComputedAllowedDeps(root)
	}
	var delta []string
	switch {
	case computedPath == "":
		fmt.Printf("allowed-deps: no computed new-allowed-deps.txt found under %s/out* — scaffolding wiring with an EMPTY delta.\n", root)
		fmt.Printf("             build once (the check may be red), then re-run: sovereign-lane-surgeon allowed-deps -out %s -name %s -apply\n", root, c.Name)
	default:
		computedSet, cerr := readAllowedDepsEntries(computedPath)
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "allowed-deps: read computed %s: %v\n", computedPath, cerr)
			return 1
		}
		for e := range computedSet {
			if !stockSet[e] {
				delta = append(delta, e)
			}
		}
		sort.Strings(delta)
		fmt.Printf("allowed-deps: delta = computed MINUS pristine stock = %d entr%s\n", len(delta), plural(len(delta), "y", "ies"))
	}

	laneTxt := filepath.Join(root, laneDir, "modules", "common", "build", "allowed_deps.txt")
	if prev, perr := readAllowedDepsEntries(laneTxt); perr == nil && computedPath != "" {
		reportDrift(prev, delta)
	}
	for _, e := range delta {
		fmt.Printf("    + %s\n", e)
	}

	laneMk := filepath.Join(root, laneDir, "modules", "common", "build", "allowed_deps.mk")
	extraRel := laneDir + "/modules/common/build/allowed_deps.txt"
	mkRel := laneDir + "/modules/common/build/allowed_deps.mk"

	txtBody := renderLaneAllowedDeps(c, laneDir, delta)
	mkBody := renderLaneAllowedDepsMk(c, laneDir, extraRel)

	products := findLaneProductMks(root, c)
	inherit := fmt.Sprintf("$(call inherit-product-if-exists, %s)", mkRel)
	comment := fmt.Sprintf("# %s Lanes — lane-sovereign allowed-deps delta (see %s/modules/common/build).", c.CamelCase, laneDir)

	if !apply {
		fmt.Printf("\n[PREVIEW — pass -apply to commit]\n")
		fmt.Printf("  write %s (%d delta entr%s)\n", relTo(root, laneTxt), len(delta), plural(len(delta), "y", "ies"))
		fmt.Printf("  write %s (EXTRA_ALLOWED_DEPS_TXT := %s)\n", relTo(root, laneMk), extraRel)
		for _, p := range products {
			if productMkWired(p, mkRel) {
				fmt.Printf("  = %s already wired — skip\n", relTo(root, p))
			} else {
				fmt.Printf("  wire %s (inherit %s)\n", relTo(root, p), mkRel)
			}
		}
		fmt.Printf("  stock %s — LEFT UNTOUCHED (pristine).\n", relTo(root, stockPath))
		return 0
	}

	if err := os.MkdirAll(filepath.Dir(laneTxt), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "allowed-deps: mkdir lane build dir: %v\n", err)
		return 1
	}
	if err := os.WriteFile(laneTxt, []byte(txtBody), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "allowed-deps: write %s: %v\n", laneTxt, err)
		return 1
	}
	if err := os.WriteFile(laneMk, []byte(mkBody), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "allowed-deps: write %s: %v\n", laneMk, err)
		return 1
	}
	fmt.Printf("\n  ✓ %s\n  ✓ %s\n", relTo(root, laneTxt), relTo(root, laneMk))

	ts := time.Now().UTC().Format("20060102T150405Z")
	wired := 0
	for _, p := range products {
		if productMkWired(p, mkRel) {
			continue
		}
		snap := filepath.Join(root, snapshotDir, ts, relTo(root, p))
		if cerr := copyFile(p, snap); cerr != nil {
			fmt.Fprintf(os.Stderr, "allowed-deps: snapshot %s failed: %v (aborting before edit)\n", relTo(root, p), cerr)
			return 1
		}
		if werr := wireProductMk(p, comment, inherit); werr != nil {
			fmt.Fprintf(os.Stderr, "allowed-deps: wire %s: %v\n", relTo(root, p), werr)
			return 1
		}
		fmt.Printf("  ✓ wired %s (snapshot → %s/%s)\n", relTo(root, p), snapshotDir+"/"+ts, relTo(root, p))
		wired++
	}
	fmt.Printf("\nallowed-deps: %d product(s) wired, stock left pristine. Verify: aosp_build_capture -lane %s -build-cmd 'm -jN apex-allowed-deps-check'\n", wired, c.Name)
	return 0
}

// readAllowedDepsEntries returns the set of non-comment, non-blank, trimmed lines.
func readAllowedDepsEntries(path string) (map[string]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, ln := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		set[t] = true
	}
	return set, nil
}

// newestComputedAllowedDeps returns the most-recently-modified new-allowed-deps.txt under
// <root>/out*/.../soong/apex/depsinfo/, or "" if none. Depth varies by out layout
// (out/, out-holo/<dev>/<variant>/, ...), so a few bounded globs beat a full out/ walk.
func newestComputedAllowedDeps(root string) string {
	suffix := filepath.Join("soong", "apex", "depsinfo", "new-allowed-deps.txt")
	var best string
	var bestMod time.Time
	for depth := 0; depth <= 4; depth++ {
		stars := make([]string, depth)
		for i := range stars {
			stars[i] = "*"
		}
		pat := filepath.Join(append([]string{root, "out*"}, append(stars, suffix)...)...)
		hits, _ := filepath.Glob(pat)
		for _, h := range hits {
			if fi, err := os.Stat(h); err == nil && fi.ModTime().After(bestMod) {
				best, bestMod = h, fi.ModTime()
			}
		}
	}
	return best
}

// findLaneProductMks returns device/*/*<DirSuffix>/aosp_*<fileToken>.mk that declare a lane
// PRODUCT_NAME. fileToken is the dir suffix with '-' -> '_' (e.g. "-holo" -> "_holo").
func findLaneProductMks(root string, c LaneConfig) []string {
	fileToken := strings.ReplaceAll(c.DirSuffix, "-", "_")
	pat := filepath.Join(root, "device", "*", "*"+c.DirSuffix, "aosp_*"+fileToken+".mk")
	hits, _ := filepath.Glob(pat)
	var out []string
	for _, h := range hits {
		b, err := os.ReadFile(h)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), "PRODUCT_NAME := aosp_") && strings.Contains(string(b), fileToken) {
			out = append(out, h)
		}
	}
	sort.Strings(out)
	return out
}

func productMkWired(path, mkRel string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(b), mkRel)
}

// wireProductMk inserts `comment` + `inherit` immediately after the PRODUCT_NAME line,
// preserving the file's mode. Idempotency is the caller's (productMkWired) responsibility.
func wireProductMk(path, comment, inherit string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	out := make([]string, 0, len(lines)+2)
	inserted := false
	for _, ln := range lines {
		out = append(out, ln)
		if !inserted && strings.HasPrefix(strings.TrimSpace(ln), "PRODUCT_NAME := aosp_") {
			out = append(out, comment, inherit)
			inserted = true
		}
	}
	if !inserted {
		return fmt.Errorf("no `PRODUCT_NAME := aosp_...` anchor line found")
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")), fi.Mode().Perm())
}

// reportDrift prints entries the previous lane file had that the new delta drops, and
// entries the new delta adds — the drift-guard signal.
func reportDrift(prev map[string]bool, delta []string) {
	now := map[string]bool{}
	for _, e := range delta {
		now[e] = true
	}
	var added, removed []string
	for _, e := range delta {
		if !prev[e] {
			added = append(added, e)
		}
	}
	for e := range prev {
		if !now[e] {
			removed = append(removed, e)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	if len(added) == 0 && len(removed) == 0 {
		fmt.Printf("allowed-deps: no drift vs the existing lane file.\n")
		return
	}
	for _, e := range added {
		fmt.Printf("allowed-deps: DRIFT +%s (new lane dep)\n", e)
	}
	for _, e := range removed {
		fmt.Printf("allowed-deps: DRIFT -%s (no longer a lane dep)\n", e)
	}
}

func renderLaneAllowedDeps(c LaneConfig, laneDir string, delta []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s Lanes — allowed-deps DELTA (lane-sovereign).\n", c.CamelCase)
	b.WriteString("#\n")
	b.WriteString("# The apex-allowed-deps check (build/soong/apex/apex_singleton.go) unions the stock\n")
	b.WriteString("# packages/modules/common/build/allowed_deps.txt with the file named by\n")
	b.WriteString("# EXTRA_ALLOWED_DEPS_TXT (this file, wired by the sibling allowed_deps.mk), then\n")
	b.WriteString("# diffs the comment-stripped, sorted-unique union against the deps the build computes.\n")
	b.WriteString("#\n")
	fmt.Fprintf(&b, "# This file carries ONLY the %s lane's own additions — the updatable-apex\n", c.CamelCase)
	b.WriteString("# dependencies introduced by the lane-forked apexes that pristine upstream does not\n")
	b.WriteString("# know about. Stock stays UNMODIFIED and tracks upstream cleanly; the lane owns its\n")
	b.WriteString("# own facts and no other lane can see them.\n")
	b.WriteString("#\n")
	b.WriteString("# GENERATED + regenerated by: sovereign-lane-surgeon allowed-deps (delta = computed MINUS stock).\n")
	b.WriteString("# Re-run it after any lane apex change; it is also the drift-guard. Do not hand-edit.\n")
	b.WriteString("\n")
	if len(delta) == 0 {
		b.WriteString("# (empty delta — this lane's forked apexes add no deps beyond pristine upstream,\n")
		b.WriteString("#  or no build has produced new-allowed-deps.txt yet.)\n")
		return b.String()
	}
	for _, e := range delta {
		b.WriteString(e)
		b.WriteString("\n")
	}
	return b.String()
}

func renderLaneAllowedDepsMk(c LaneConfig, laneDir, extraRel string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s Lanes — wire the lane's allowed-deps delta into the apex-allowed-deps check.\n", c.CamelCase)
	b.WriteString("#\n")
	b.WriteString("# EXTRA_ALLOWED_DEPS_TXT is read by build/make/core/soong_config.mk (as\n")
	b.WriteString("# ExtraAllowedDepsTxt) and consumed in build/soong/apex/apex_singleton.go, where it\n")
	b.WriteString("# is APPENDED to the stock allowed_deps source and unioned before the diff. Pointing\n")
	b.WriteString("# it at the lane-owned delta lets the lane-forked updatable apexes be allow-listed\n")
	b.WriteString("# WITHOUT modifying stock packages/modules/common/build/allowed_deps.txt.\n")
	b.WriteString("#\n")
	fmt.Fprintf(&b, "# Inherited by each %s device product .mk. Keeping the wiring in the lane tree is\n", c.CamelCase)
	b.WriteString("# what makes the lane sovereign over its own allowed-deps: stock stays pristine.\n")
	fmt.Fprintf(&b, "EXTRA_ALLOWED_DEPS_TXT := %s\n", extraRel)
	return b.String()
}

func relTo(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return r
	}
	return p
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
