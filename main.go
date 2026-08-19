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

// Command sovereign-lane-surgeon operationalizes the sovereign-lane bring-to-green
// method + blocker taxonomy. SOURCE OF TRUTH: holo_lane_sovereignty_handoff_20260711.md
// — this tool is that doc's executable form, kept in sync with it:
//
//	§22 blocker taxonomy (A–F) ↔ taxonomy.go
//	§23.1 create-scaffolder     ↔ create.go / scaffold.go (+ soongpatch.go, bpmirror.go)
//	§23.2 audit + doctor        ↔ audit.go
//
// It is portable: parameterized by a lane config, it runs on any AOSP tree (this repo, a
// workstation checkout, or a different Android-version repo). CONSOLIDATED + fully self-contained
// (T-directed 2026-07-12): ZERO external deps — it does NOT depend on the fragmented workspace lane
// toolkits, and the canonical Soong Blueprint parser it uses for AST-safe .bp rewrites (requalify.go)
// is VENDORED at internal/blueprint/parser (Apache-2.0; upstream can't be `go get`'d — its
// pathtools/testdata carries glob-char filenames that break `go mod`). So it `go build`s standalone.
//
// Subcommands:
//
//	audit  -report <dir|json>   classify a build-capture run's failures by the §22 taxonomy
//	verify -report <dir|json>   print the authoritative run-dir verdict (dir_status), never exit code
//	create                      lane scaffolder (§23.1): products + route manifest, stages soong patches
//	apply  -out <aosp-root>     commit the staged soong patches (snapshots first) — preview-then-apply
//	uninstall -name <l> -out <r> fully reverse a seeded lane (byte-identical revert, multi-lane-safe)
//	doctor -report <dir|json>   suggest/apply the recipe per classified failure (§23.2) [foundation]
//	version
//
// The audit is the always-safe read-only core: it turns "the build failed" into
// "module X failed with class Y → recipe Z", the classification this project did by
// hand across every lane green-up.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const version = "0.3.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "audit":
		os.Exit(cmdAudit(args))
	case "verify":
		os.Exit(cmdVerify(args))
	case "create":
		os.Exit(cmdCreate(args))
	case "apply":
		os.Exit(cmdApply(args))
	case "uninstall":
		os.Exit(cmdUninstall(args))
	case "requalify":
		os.Exit(cmdRequalify(args))
	case "rename-module":
		os.Exit(cmdRenameModule(args))
	case "drop-dep":
		os.Exit(cmdDropDep(args))
	case "reexport":
		os.Exit(cmdReexport(args))
	case "doctor":
		os.Exit(cmdDoctor(args))
	case "version", "-v", "--version":
		fmt.Println("sovereign-lane-surgeon", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `sovereign-lane-surgeon — bring a sovereign AOSP lane to a green eng image.

USAGE:
  sovereign-lane-surgeon <subcommand> [flags]

SUBCOMMANDS:
  audit   -report <dir|json>   Classify a build-capture run's failures by the §22 blocker taxonomy.
                               Emits: module | class | layer | auto? | recipe. Read-only.
  verify  -report <dir|json>   Print the run-dir verdict (dir_status) — the authoritative pass/fail,
                               never the process exit code.
  create                       Interactive scaffolder for a new sovereign lane (§23.1). With
                               -out, generates device/emu products + route manifest and STAGES
                               the in-place soong patches (preview-then-apply).
  uninstall -name <l> -out <root>  Fully reverse a seeded lane (dirs + shared products + AndroidProducts.mk
                               + soong patches, all surgical/multi-lane-safe). Byte-identical revert.
  apply   -out <aosp-root>     Commit the soong patches staged by 'create -out' onto the tree
                               (snapshots each live file first). Review the create diffs first.
  requalify -name <l> -out <r> AST-safe //<root>/… → //<root>-<lane>/… delane over an existing lane
                               (retire vestigial stock-path refs). -prefix for the rename model.
  rename-module -name <l> -out <r>  AST-safe module-name rewrite across the lane tree (def + every
                               dep-ref, form-preserving). -map "old=new,…" | -deprefix <P> -modules a,b.
                               PRIMARY USE: Model-A keep-name conformity — de-prefix a framework-class /
                               shared-infra module a lane wrongly prefixed, so stock consumers resolve it.
  drop-dep -name <l> -out <r>  AST-safe removal of named dep entries from every dep list + :module srcs
                               ref across the lane tree. -deps "//root/dir:mod,bare-name,…". PRIMARY USE:
                               no-Compose lane SystemUI bps carry stale external refs to libs that resolve
                               internally (Dagger/FQN) — drop the ref (points at an empty/absent lib).
  doctor  -report <dir|json>   Per classified failure, print (or apply) its recipe (§23.2).
  version

The taxonomy + method are documented in holo_lane_sovereignty_handoff_20260711.md §22.
`)
}

// reportFlag adds the shared -report flag to a flagset.
func reportFlag(fs *flag.FlagSet) *string {
	return fs.String("report", "", "build-capture run dir or terminal_log.json path")
}

func cmdVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	report := reportFlag(fs)
	_ = fs.Parse(args)
	if *report == "" {
		fmt.Fprintln(os.Stderr, "verify: -report is required")
		return 2
	}
	r, err := LoadReport(*report)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify:", err)
		return 1
	}
	fmt.Printf("lane=%s lunch=%s\n", r.Lane, r.LunchTarget)
	fmt.Printf("dir_status=%s success=%v exit_code=%d duration=%.0fs\n", r.DirStatus, r.Success, r.ExitCode, r.DurationSec)
	if r.green() {
		fmt.Println("VERDICT: GREEN (full_completed)")
		return 0
	}
	fmt.Println("VERDICT: NOT GREEN — run `audit` to classify the failures")
	return 1
}

func cmdCreate(args []string) int {
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	out := fs.String("out", "", "AOSP root to generate scaffolding into (default: dry-run — print the plan)")
	name := fs.String("name", "", "lane name(s), lowercase — up to 3 comma-separated (e.g. testing1,testing2,testing3); non-interactive")
	rename := fs.Bool("rename", false, "rename+overrides identity model (NexusM); default is keep-name+finder (Holo)")
	goldfish := fs.Bool("goldfish", true, "scaffold goldfish emulator products (sdk_phone64/sdk_tablet)")
	cuttlefish := fs.Bool("cuttlefish", true, "note cuttlefish products in the plan")
	devices := fs.String("devices", "", "comma-separated device names to note (e.g. lynx,tangorpro)")
	fork := fs.String("fork", "", "comma-separated subtrees to clone into the lane tree (e.g. frameworks/base/core/res,packages/apps/Settings). With -from, may also name LANE roots (frameworks-holo,packages-holo).")
	from := fs.String("from", "", "seed this lane from an EXISTING LANE instead of stock (e.g. -from holo): defaults -fork to that lane's roots, repoints its labels/paths onto the new lane, and inherits its curated bp drop-list. A lane-sourced fork inherits the source's directory RELOCATIONS, which the per-file drop rule cannot derive.")
	forkExclude := fs.String("fork-exclude", "", "comma-separated subpaths to LEAVE stock inside a fork (namespace-complex, e.g. frameworks/base/packages/SystemUI)")
	prefixDirs := fs.String("prefix-dirs", "", "physical directory prefix when KeepName is false (e.g. NexusM)")
	noCompose := fs.Bool("no-compose", false, "experimental: bypass AndroidX/Compose (Nexus-Modern style — leave Compose/AndroidX subtrees stock, auto-drop their dep refs, scope SystemUI srcs to the re-authored kotlin/ tree)")
	_ = fs.Parse(args)

	printCreateIntro()

	var devs, forks []string
	for _, d := range strings.Split(*devices, ",") {
		if s := strings.TrimSpace(d); s != "" {
			devs = append(devs, s)
		}
	}
	for _, f := range strings.Split(*fork, ",") {
		if s := strings.TrimSpace(f); s != "" {
			forks = append(forks, s)
		}
	}
	var forkExcludes []string
	for _, f := range strings.Split(*forkExclude, ",") {
		if s := strings.TrimSpace(f); s != "" {
			forkExcludes = append(forkExcludes, s)
		}
	}

	var cfgs []LaneConfig
	if *name != "" {
		var names []string
		for _, n := range strings.Split(*name, ",") {
			if s := strings.TrimSpace(n); s != "" {
				names = append(names, s)
			}
		}
		if len(names) > 3 {
			fmt.Fprintf(os.Stderr, "create: up to 3 lanes at a time (got %d)\n", len(names))
			return 2
		}
		// A lane name is a SUFFIX in the finder's path predicates, so it must be unique against
		// every directory name already in the tree. Checked before any file is written: a bad name
		// is only visible later as an unrelated lane failing on a module it never referenced.
		if *out != "" {
			for _, nm := range names {
				if off, total := checkLaneSuffixCollision(*out, nm); total > 0 {
					fmt.Fprintf(os.Stderr, "create: REFUSED — lane name %q collides with %d existing director%s ending in \"-%s\".\n",
						nm, total, map[bool]string{true: "y", false: "ies"}[total == 1], nm)
					for _, o := range off {
						fmt.Fprintf(os.Stderr, "    %s\n", o)
					}
					if total > len(off) {
						fmt.Fprintf(os.Stderr, "    ... and %d more\n", total-len(off))
					}
					fmt.Fprintf(os.Stderr, "  The finder drops any path component ending in the lane suffix, so EVERY lane in this\n"+
						"  tree would stop seeing those directories. Choose a name that no directory ends with.\n")
					return 2
				}
			}
		}
		for _, nm := range names {
			c := deriveLane(nm, !*rename, devs, *goldfish, *cuttlefish, *prefixDirs)
			c.Forks = forks
			c.ForkExclude = forkExcludes
			c.NoCompose = *noCompose
			c.FromLane = *from
			// A lane-sourced fork defaults to cloning the source lane WHOLE. Naming the roots by
			// hand is the common case and easy to get subtly wrong (a partial clone silently
			// falls through to stock for whatever was missed).
			if *from != "" && len(forks) == 0 {
				c.Forks = []string{"frameworks-" + *from, "packages-" + *from}
			}
			cfgs = append(cfgs, c)
		}
	} else {
		c, ok := gatherLaneConfigInteractive()
		if !ok {
			fmt.Println("(no tty and no -name — showing the static flow; pass -name <lane> to generate non-interactively)")
			printScaffoldPlan(deriveLane("<lane>", true, []string{"<device>"}, true, true, ""))
			return 0
		}
		c.Forks = forks
		c.ForkExclude = forkExcludes
		cfgs = append(cfgs, c)
		fmt.Println()
	}

	if *out == "" {
		for _, cfg := range cfgs {
			printScaffoldPlan(cfg)
		}
		return 0
	}
	rc := 0
	for i, cfg := range cfgs {
		if len(cfgs) > 1 {
			fmt.Printf("\n═════════ Lane %d/%d: %s ═════════\n", i+1, len(cfgs), cfg.Name)
		}
		if r := writeScaffold(cfg, *out); r != 0 {
			rc = r
		}
	}
	if len(cfgs) > 1 {
		fmt.Printf("\nAll %d lanes staged. One `sovereign-lane-surgeon apply -out %s` commits the accumulated soong patches for all.\n", len(cfgs), *out)
	}
	return rc
}

func cmdApply(args []string) int {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)
	out := fs.String("out", "", "AOSP root holding a .sld-staging dir from a prior `create -out`")
	_ = fs.Parse(args)
	if *out == "" {
		fmt.Fprintln(os.Stderr, "apply: -out <aosp-root> is required")
		return 2
	}
	return applyStaged(*out)
}

func cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	report := reportFlag(fs)
	apply := fs.Bool("apply", false, "apply the recipe (foundation: not yet wired; prints intent)")
	_ = fs.Parse(args)
	if *report == "" {
		fmt.Fprintln(os.Stderr, "doctor: -report is required")
		return 2
	}
	r, err := LoadReport(*report)
	if err != nil {
		fmt.Fprintln(os.Stderr, "doctor:", err)
		return 1
	}
	findings := Classify(r)
	if len(findings) == 0 {
		fmt.Println("no failures classified — nothing to doctor")
		return 0
	}
	for _, f := range findings {
		if f.Class == nil {
			fmt.Printf("[TRIAGE] %s (unclassified)\n  → inspect the log; extend the taxonomy if it's a new class\n", f.Module)
			continue
		}
		verb := "SUGGEST"
		if *apply && f.Class.Auto {
			verb = "APPLY(v2)"
		}
		fmt.Printf("[%s] %s (%s)\n  → %s\n", verb, f.Module, f.Class.Name, f.Class.Recipe)
	}
	return 0
}
