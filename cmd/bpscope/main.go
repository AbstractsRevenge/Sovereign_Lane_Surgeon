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

// bpscope: AST-grounded scope facts for Android.bp files, using the Surgeon's own
// Blueprint parser. Per file it reports the top-level VARIABLES it DEFINES (Assignments),
// the variables it REFERENCES but does not define locally (via ParseAndEval with an empty
// scope — the exact "undefined variable" check Soong's bootstrap runs), and its module names.
// This is the instrument for reasoning about lane finder drops on variable scope, not grep.
package main

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/internal/blueprint/parser"
)

const marker = "undefined variable "

func main() {
	for _, path := range os.Args[1:] {
		b, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("== %s\n   READ ERROR: %v\n", path, err)
			continue
		}

		// Definitions + module names (plain Parse: no evaluation, so no scope needed).
		file, perrs := parser.Parse(path, bytes.NewReader(b))
		var defs, mods []string
		if file != nil {
			for _, d := range file.Defs {
				switch n := d.(type) {
				case *parser.Assignment:
					defs = append(defs, n.Name)
				case *parser.Module:
					nm := n.Name()
					if nm == "" {
						nm = "(" + n.Type + ")"
					}
					mods = append(mods, nm)
				}
			}
		}

		// External references: evaluate against an EMPTY scope. Every variable the file
		// references but does not itself define surfaces as "undefined variable X" — i.e. a
		// scope dependency on a parent-directory bp.
		ext := map[string]bool{}
		_, everrs := parser.ParseAndEval(path, bytes.NewReader(b), parser.NewScope(nil))
		for _, e := range everrs {
			s := e.Error()
			if i := strings.Index(s, marker); i >= 0 {
				name := strings.TrimSpace(s[i+len(marker):])
				if j := strings.IndexAny(name, " \t\r\n"); j >= 0 {
					name = name[:j]
				}
				if name != "" {
					ext[name] = true
				}
			}
		}
		var extl []string
		for k := range ext {
			extl = append(extl, k)
		}
		sort.Strings(defs)
		sort.Strings(extl)
		sort.Strings(mods)

		fmt.Printf("== %s\n", path)
		fmt.Printf("   defines-var:   %v\n", defs)
		fmt.Printf("   refs-external: %v\n", extl)
		fmt.Printf("   modules(%d):    %v\n", len(mods), mods)
		if len(perrs) > 0 {
			fmt.Printf("   parse-errs:    %d (e.g. %v)\n", len(perrs), perrs[0])
		}
	}
}
