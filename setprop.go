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
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/internal/blueprint/parser"
)

// setprop.go — the `set-prop` subcommand: set a BOOLEAN property on named lane modules, AST-safe via the
// Blueprint parser (never a hand/regex edit). Use it for the narrow cases where a lane rename breaks an
// inherited-property assumption a Soong analyzer makes by name/graph and the value must be stated
// explicitly on the module (e.g. cmake_snapshot_supported:true on the renamed binder RPC libs that a
// cc_cmake_snapshot host export depends on — stock inherits it through the defaults chain, the renamed
// chain does not propagate it to the leaf here). Only bool props are supported; the target modules are
// named exactly. Dry-run by default; -apply writes. Mutation only reaches the lane's own dirs.
func cmdSetProp(args []string) int {
	fset := flag.NewFlagSet("set-prop", flag.ExitOnError)
	name := fset.String("name", "", "lane name (e.g. holo2)")
	out := fset.String("out", "", "AOSP root")
	modules := fset.String("modules", "", "comma-separated exact module names to set the property on")
	prop := fset.String("prop", "", "boolean property name to set (e.g. cmake_snapshot_supported)")
	val := fset.Bool("value", true, "boolean value to set (default true)")
	remove := fset.Bool("remove", false, "remove the property instead of setting it")
	apply := fset.Bool("apply", false, "write the change (default: dry-run — report only)")
	_ = fset.Parse(args)
	if *name == "" || *out == "" || *modules == "" || *prop == "" {
		fmt.Fprintln(os.Stderr, "set-prop: -name, -out, -modules and -prop are required")
		return 2
	}
	targets := map[string]bool{}
	for _, m := range strings.Split(*modules, ",") {
		if m = strings.TrimSpace(m); m != "" {
			targets[m] = true
		}
	}
	if len(targets) == 0 {
		return 0
	}

	roots := []string{"frameworks-" + *name, "packages-" + *name}
	if ents, err := os.ReadDir(filepath.Join(*out, "device", "google")); err == nil {
		for _, e := range ents {
			if e.IsDir() && strings.HasSuffix(e.Name(), "-"+*name) {
				roots = append(roots, filepath.Join("device", "google", e.Name()))
			}
		}
	}

	changed, failed := 0, 0
	hit := map[string]bool{}
	var report []string
	for _, root := range roots {
		_ = filepath.WalkDir(filepath.Join(*out, root), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !isBpFile(d.Name()) {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			file, errs := parser.Parse("", bytes.NewReader(b))
			if len(errs) > 0 || file == nil {
				return nil
			}
			bpChanged := false
			for _, def := range file.Defs {
				mod, ok := def.(*parser.Module)
				if !ok {
					continue
				}
				var nm string
				for _, pr := range mod.Properties {
					if pr.Name == "name" {
						if s, ok := pr.Value.(*parser.String); ok {
							nm = s.Value
						}
					}
				}
				if nm == "" || !targets[nm] {
					continue
				}
				hit[nm] = true
				rel, _ := filepath.Rel(*out, p)
				if *remove {
					if removeProp(mod, *prop) {
						bpChanged = true
						report = append(report, fmt.Sprintf("  %s: removed %s (from %s)", filepath.ToSlash(rel), *prop, nm))
					}
					continue
				}
				added := setOrAddBool(mod, *prop, *val)
				bpChanged = true
				verb := "set"
				if added {
					verb = "added"
				}
				report = append(report, fmt.Sprintf("  %s: %s %s:%t (%s)", filepath.ToSlash(rel), verb, *prop, *val, nm))
			}
			if !bpChanged {
				return nil
			}
			outB, perr := parser.Print(file)
			if perr != nil {
				failed++
				return nil
			}
			if !*apply {
				changed++
				return nil
			}
			if os.WriteFile(p, outB, d.Type().Perm()|0o200) == nil {
				changed++
			} else {
				failed++
			}
			return nil
		})
	}
	sort.Strings(report)
	fmt.Printf("set-prop: %s:%t on %d module(s) matched across %d bp(s) (lane %q)\n", *prop, *val, len(hit), changed, *name)
	for _, r := range report {
		fmt.Println(r)
	}
	for m := range targets {
		if !hit[m] {
			fmt.Printf("  (warning: module %q not found in lane)\n", m)
		}
	}
	if !*apply {
		fmt.Println("(dry-run; pass -apply to write)")
	}
	if failed > 0 {
		return 1
	}
	return 0
}
