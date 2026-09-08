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
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// depledger.go — the `dep-ledger` subcommand: classify every undefined dependency (the
// `undefined-deps` census) into an ACTIONABLE disposition, so a lane's unresolved refs become a
// fork-vs-wire worklist instead of a flat list. Same AST parser as undefined-deps; the fork-vs-share
// decision is delegated to a PROVEN-GREEN reference lane (default: holo) as the oracle — if the green
// lane forks a module, the audited lane should provide it too; if only stock defines it, the gap is
// routing, not a missing fork. This removes the human judgment my earlier regex pass guessed at.
//
// Dispositions (evaluated in this order; first match wins):
//   LANE-BUG  the ref names the lane's OWN module with wrong casing (Nexusm... should be NexusM...)
//   DEAD      retired product-lane scaffolding (product_fork_src_* / productowner_*) — drop the ref
//   REPOINT   the lane already forks it under a renamed form (NexusMX / nexusm-X / X-nexusm) — route the ref
//   LOAD      the lane defines it keep-name but the defining bp is not loaded (missing build= include / dropped)
//   FORK      the green oracle lane forks it and the audited lane does not — author a lane fork (UX-bearing)
//   STOCK-GAP stock defines it but it is not resolving for the lane (dropped parallel / not loaded) — route or keep-name
//   MISSING   defined nowhere in lane / oracle / stock — removed-legacy or genuinely absent — drop/investigate
// Each row also carries a TEST-ONLY flag (all referencing bps live under a tests/ dir → does not gate the image).

type ledgerRow struct {
	Name        string   `json:"name"`
	Disposition string   `json:"disposition"`
	Detail      string   `json:"detail"`
	Bps         int      `json:"bps"`
	TestOnly    bool     `json:"test_only"`
	Refs        []string `json:"refs,omitempty"`
}

// moduleSet walks the given top-level roots (under outRoot) and returns every module name declared
// in ANY .bp file — the OWNERSHIP set, not the load-reachable set. It intentionally scans every .bp
// (not just Android.bp + its `build=` includes), because a module the lane declares in a bp that is
// NOT wired in (e.g. AconfigFlags.bp with no `build=` entry) is still OWNED by the lane; that gap is
// a LOAD disposition, not a missing fork. `undefinedDeps` already decides load-reachability.
func moduleSet(outRoot string, roots []string) map[string]bool {
	set := map[string]bool{}
	for _, root := range roots {
		filepath.Walk(filepath.Join(outRoot, root), func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || !strings.HasSuffix(p, ".bp") {
				return nil
			}
			names, _ := bpModules(p)
			for n := range names {
				set[n] = true
			}
			return nil
		})
	}
	return set
}

// nexusmCasedRe matches a lane self-reference carrying the wrong casing (lowercase 'm'); the
// convention is NexusM<Name>, so a leading "Nexusm" (or ":Nexusm") is a rename-follow-through bug.
var nexusmCasedRe = regexp.MustCompile(`^:?Nexusm`)

// renameForms are the ways a rename-model lane may have re-declared a canonical module name.
// It never returns the identity name — only genuinely-renamed forms — so a keep-name module the
// lane owns is classified LOAD (its bp isn't loaded), not mistaken for a rename (REPOINT).
func renameForms(name string) []string {
	forms := []string{"NexusM" + name, "nexusm-" + name, name + "-nexusm"}
	if r := strings.Replace(name, "com.android.", "nexusm-", 1); r != name {
		forms = append(forms, r)
	}
	return forms
}

// classifyDep returns the disposition + a one-line detail for one undefined dependency name,
// given the lane's own declared modules, the green oracle lane's modules, and stock's modules.
func classifyDep(name string, lane, oracle, stock map[string]bool) (string, string) {
	if nexusmCasedRe.MatchString(name) {
		return "LANE-BUG", "lane's own module, wrong casing (Nexusm -> NexusM)"
	}
	if strings.Contains(name, "product_fork_src_") || strings.Contains(name, "productowner_") {
		return "DEAD", "retired product-lane scaffolding; drop the ref"
	}
	for _, f := range renameForms(name) {
		if definedOrDerived(f, lane) {
			return "REPOINT", "lane forks it as " + f + "; route the ref"
		}
	}
	if definedOrDerived(name, lane) {
		return "LOAD", "lane owns it keep-name; defining bp not loaded (build= / drop)"
	}
	if definedOrDerived(name, oracle) || definedOrDerived("Holo"+name, oracle) {
		return "FORK", "green oracle forks it; lane missing -> author a lane fork"
	}
	if definedOrDerived(name, stock) {
		return "STOCK-GAP", "stock defines it; not resolving for lane -> route or keep-name"
	}
	return "MISSING", "not in lane/oracle/stock; likely removed-legacy -> drop/investigate"
}

// isTestOnly reports whether every referencing bp lives under a tests/ or test/ directory.
func isTestOnly(bps []string) bool {
	if len(bps) == 0 {
		return false
	}
	for _, b := range bps {
		if !strings.Contains(b, "/tests/") && !strings.Contains(b, "/test/") {
			return false
		}
	}
	return true
}

var dispOrder = []string{"FORK", "STOCK-GAP", "REPOINT", "LOAD", "LANE-BUG", "DEAD", "MISSING"}

func cmdDepLedger(args []string) int {
	fs := flag.NewFlagSet("dep-ledger", flag.ExitOnError)
	name := fs.String("name", "", "lane name to audit (e.g. nexusm)")
	out := fs.String("out", "", "AOSP root")
	oracleLane := fs.String("oracle", "holo", "proven-green reference lane used as the fork-vs-share oracle")
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	_ = fs.Parse(args)
	if *name == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "dep-ledger: -name and -out are required")
		return 2
	}

	reports := undefinedDeps(*out, *name)
	lane := moduleSet(*out, laneBpRoots(*out, *name))
	oracle := moduleSet(*out, laneBpRoots(*out, *oracleLane))
	stock := moduleSet(*out, []string{"frameworks", "packages"})

	rows := make([]ledgerRow, 0, len(reports))
	for _, r := range reports {
		disp, det := classifyDep(r.Name, lane, oracle, stock)
		rows = append(rows, ledgerRow{
			Name: r.Name, Disposition: disp, Detail: det,
			Bps: len(r.Bps), TestOnly: isTestOnly(r.Bps), Refs: r.Bps,
		})
	}
	rank := map[string]int{}
	for i, d := range dispOrder {
		rank[d] = i
	}
	sort.Slice(rows, func(i, j int) bool {
		if rank[rows[i].Disposition] != rank[rows[j].Disposition] {
			return rank[rows[i].Disposition] < rank[rows[j].Disposition]
		}
		return rows[i].Bps > rows[j].Bps
	})

	if *asJSON {
		b, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Println(string(b))
		return 0
	}

	total := map[string]int{}
	ship := map[string]int{} // non-test-only (image-gating)
	fmt.Printf("dep-ledger: %d undefined dep(s) for lane %q (oracle=%s)\n\n", len(rows), *name, *oracleLane)
	fmt.Printf("  %-9s %4s %3s  %-46s %s\n", "DISP", "bp", "tst", "module", "detail")
	for _, r := range rows {
		total[r.Disposition]++
		flag := ""
		if r.TestOnly {
			flag = "T"
		} else {
			ship[r.Disposition]++
		}
		fmt.Printf("  %-9s %4d %3s  %-46s %s\n", r.Disposition, r.Bps, flag, r.Name, r.Detail)
	}
	fmt.Println("\n=== ledger summary (ship = image-gating, non-test-only) ===")
	for _, k := range dispOrder {
		if total[k] > 0 {
			fmt.Printf("  %-9s %3d  (ship %d)\n", k, total[k], ship[k])
		}
	}
	return 0
}
