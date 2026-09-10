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
	"flag"
	"fmt"
	"os"
)

// cmdFixLaneDefects applies laneToolFixes to an ALREADY-scaffolded lane, standalone.
//
// runFixLaneCreatedDefects otherwise fires only from the create/scaffold flow (scaffold.go), so a
// lane that was seeded before a given fix entry existed never receives it. This exposes the same
// mechanism for the existing tree: byte-exact, match-or-skip, per-entry already-applied detection —
// no re-scaffold. Same code path the tests exercise (LaneConfig{Name} is the only field it reads).
func cmdFixLaneDefects(args []string) int {
	fset := flag.NewFlagSet("fix-lane-defects", flag.ExitOnError)
	name := fset.String("name", "", "lane name, lowercase (e.g. holo2)")
	out := fset.String("out", "", "AOSP root holding frameworks-<lane>/")
	_ = fset.Parse(args)
	if *name == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "fix-lane-defects: -name <lane> and -out <aosp-root> are required")
		return 2
	}
	n := runFixLaneCreatedDefects(LaneConfig{Name: *name}, *out)
	fmt.Printf("\nfix-lane-defects: %d fix(es) applied to lane %q\n", n, *name)
	return 0
}
