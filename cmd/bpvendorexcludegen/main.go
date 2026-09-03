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
	"os"
	"path/filepath"
	"strings"

	parser "github.com/AbstractsRevenge/sovereign_lane_surgeon/internal/blueprint/parser"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <AOSP_ROOT>\n", os.Args[0])
		os.Exit(1)
	}
	aospRoot := os.Args[1]

	searchDirs := []string{"frameworks", "packages"}

	vendoredDirs := map[string]bool{}

	for _, d := range searchDirs {
		rootDir := filepath.Join(aospRoot, d)
		filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || filepath.Base(path) != "Android.bp" {
				return nil
			}

			b, err := os.ReadFile(path)
			if err != nil {
				return nil
			}

			file, errs := parser.Parse("", bytes.NewReader(b))
			if len(errs) > 0 {
				return nil
			}

			hasVendoredModule := false
			for _, def := range file.Defs {
				mod, ok := def.(*parser.Module)
				if !ok {
					continue
				}

				for _, prop := range mod.Properties {
					if prop.Name == "name" {
						if ns, ok := prop.Value.(*parser.String); ok {
							name := ns.Value
							lowerName := strings.ToLower(name)
							if strings.HasPrefix(lowerName, "androidx.") ||
								strings.HasPrefix(lowerName, "kotlin") ||
								strings.Contains(lowerName, "compose") {
								hasVendoredModule = true
								break
							}
						}
					}
				}
				if hasVendoredModule {
					break
				}
			}

			if hasVendoredModule {
				relDir, _ := filepath.Rel(aospRoot, filepath.Dir(path))
				vendoredDirs[relDir] = true
			}
			return nil
		})
	}

	for dir := range vendoredDirs {
		fmt.Println(dir)
	}
}
