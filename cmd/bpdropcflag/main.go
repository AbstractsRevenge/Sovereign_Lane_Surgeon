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

// Command bpdropcflag removes exact string entries from every cflags / cppflags / conlyflags list
// in the given Android.bp files, at any nesting depth (arch:, target:, soong_config_variables:
// maps and `[...] + select(...)` concatenations). AST-safe via the vendored Blueprint parser
// (HARD RULE 3 — no regex on .bp source), form-preserving via parser.Print, idempotent.
//
// Origin: android-17 Soong rejects `-pedantic` in cflags ("Illegal flag"), which AOSP 15 device
// trees (device/google/gs-common/gsa) still carry. Sibling of cmd/bpvendorexcludegen. The edit
// itself lives in internal/bpflags, shared with `create -stock`, which reads the illegal list out
// of the target tree's Soong and applies it to every mirrored subtree automatically.
//
//	bpdropcflag -flags "-pedantic,-Wextra" path/to/Android.bp [...]
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/abstractsrevenge/sovereign_lane_surgeon/internal/bpflags"
)

func main() {
	fl := flag.String("flags", "", "comma-separated exact cflag strings to remove (e.g. -pedantic)")
	flag.Parse()
	drop := map[string]bool{}
	for _, f := range strings.Split(*fl, ",") {
		if f = strings.TrimSpace(f); f != "" {
			drop[f] = true
		}
	}
	if len(drop) == 0 || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: bpdropcflag -flags \"-pedantic,...\" <Android.bp>...")
		os.Exit(2)
	}
	rc := 0
	for _, path := range flag.Args() {
		b, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", path, err)
			rc = 1
			continue
		}
		out, changed, err := bpflags.Drop(b, drop)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", path, err)
			rc = 1
			continue
		}
		if !changed {
			fmt.Printf("  = %s: no matching flag\n", path)
			continue
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: write: %v\n", path, err)
			rc = 1
			continue
		}
		fmt.Printf("  ✓ %s\n", path)
	}
	os.Exit(rc)
}
