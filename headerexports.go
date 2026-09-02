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
	"os"
	"path/filepath"
	"strings"

	parser "github.com/abstractsrevenge/sovereign_lane_surgeon/internal/blueprint/parser"
)

// headerexports.go — target-compat operation 5: a transitive header export the target dropped.
//
// WHY (observed 2026-09-02, cheetah `m droid superimage`, Build Capture run 130400Z):
// hardware/google/graphics/common/libion/ion.cpp includes <ion/ion.h> and its module declares only
// liblog + libdmabufheap. In android-15 that was enough: libdmabufheap linked libion statically and
// re-exported its headers (export_static_lib_headers: ["libion"]). android-17's libdmabufheap
// dropped libion, so the include fails ("'ion/ion.h' file not found") while libion — and its
// vendor-available libion_headers — still exist in the tree. The code calls no ION function, so the
// missing piece is exactly the header library. The table below names such lost re-exports; each is
// applied only when the target's provider really no longer exports the library (parsed from the
// provider's Blueprint) and the replacement header library exists, to modules that depend on the
// provider AND whose sources include a header under the prefix.
type lostHeaderExport struct {
	Provider     string // the library that used to re-export
	ProviderBp   string // its Android.bp, repo-relative
	Exported     string // the library it exported (in export_*_lib_headers)
	HeaderPrefix string // includes that need it, e.g. "ion/"
	HeaderLib    string // the header library to add
	HeaderLibBp  string // where HeaderLib is defined, repo-relative
}

var lostHeaderExports = []lostHeaderExport{
	{Provider: "libdmabufheap", ProviderBp: "system/memory/libdmabufheap/Android.bp", Exported: "libion",
		HeaderPrefix: "ion/", HeaderLib: "libion_headers", HeaderLibBp: "system/memory/libion/Android.bp"},
}

// bpDefinesModule reports whether the Blueprint at rel (under outRoot) defines a module named name.
func bpDefinesModule(outRoot, rel, name string) bool {
	b, err := os.ReadFile(filepath.Join(outRoot, filepath.FromSlash(rel)))
	if err != nil {
		return false
	}
	file, errs := parser.Parse(rel, bytes.NewReader(b))
	if len(errs) > 0 {
		return false
	}
	for _, def := range file.Defs {
		if m, ok := def.(*parser.Module); ok && m.Name() == name {
			return true
		}
	}
	return false
}

// providerStillExports reports whether the provider module in its Blueprint still lists exported
// in any export_*_lib_headers property.
func providerStillExports(outRoot string, e lostHeaderExport) bool {
	b, err := os.ReadFile(filepath.Join(outRoot, filepath.FromSlash(e.ProviderBp)))
	if err != nil {
		return true // unknown tree state: change nothing
	}
	file, errs := parser.Parse(e.ProviderBp, bytes.NewReader(b))
	if len(errs) > 0 {
		return true
	}
	for _, def := range file.Defs {
		m, ok := def.(*parser.Module)
		if !ok || m.Name() != e.Provider {
			continue
		}
		for _, pr := range m.Properties {
			if !strings.HasPrefix(pr.Name, "export_") || !strings.HasSuffix(pr.Name, "_lib_headers") {
				continue
			}
			vals := map[string]bool{}
			collectStrings(pr.Value, vals)
			if vals[e.Exported] {
				return true
			}
		}
	}
	return false
}

// moduleSrcsInclude reports whether any file listed in the module's srcs (relative to bpDir)
// contains an #include of a header under prefix. Plain text scan of source files, not build files.
func moduleSrcsInclude(m *parser.Module, bpDir, prefix string) bool {
	srcs := map[string]bool{}
	for _, pr := range m.Properties {
		if pr.Name == "srcs" {
			collectStrings(pr.Value, srcs)
		}
	}
	for s := range srcs {
		if strings.HasPrefix(s, ":") || strings.Contains(s, "*") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(bpDir, filepath.FromSlash(s)))
		if err != nil {
			continue
		}
		if bytes.Contains(b, []byte("#include <"+prefix)) || bytes.Contains(b, []byte("#include \""+prefix)) {
			return true
		}
	}
	return false
}

// addToListProp appends val to the module's list property prop (creating it), unless present.
func addToListProp(m *parser.Module, prop, val string) bool {
	for _, pr := range m.Properties {
		if pr.Name != prop {
			continue
		}
		lst, ok := pr.Value.(*parser.List)
		if !ok {
			return false
		}
		for _, v := range lst.Values {
			if s, ok := v.(*parser.String); ok && s.Value == val {
				return false
			}
		}
		lst.Values = append(lst.Values, &parser.String{Value: val})
		return true
	}
	m.Properties = append(m.Properties, &parser.Property{Name: prop, Value: &parser.List{Values: []parser.Expression{&parser.String{Value: val}}}})
	return true
}

func addLostHeaderLibs(outRoot string, roots []string, r *compatReport) {
	for _, e := range lostHeaderExports {
		if providerStillExports(outRoot, e) {
			continue // the target still re-exports: nothing to add
		}
		if !bpDefinesModule(outRoot, e.HeaderLibBp, e.HeaderLib) {
			r.Notes = append(r.Notes, "header libs: "+e.Provider+" no longer exports "+e.Exported+" and the target has no "+e.HeaderLib+" — modules including <"+e.HeaderPrefix+"…> will fail to compile")
			continue
		}
		for _, root := range roots {
			walkFiles(filepath.Join(outRoot, filepath.FromSlash(root)), isBlueprint, func(p string) {
				b, rerr := os.ReadFile(p)
				if rerr != nil {
					return
				}
				file, errs := parser.Parse(p, bytes.NewReader(b))
				if len(errs) > 0 {
					return
				}
				changed := false
				for _, def := range file.Defs {
					m, ok := def.(*parser.Module)
					if !ok || !strings.HasPrefix(m.Type, "cc_") {
						continue
					}
					deps := map[string]bool{}
					for _, pr := range m.Properties {
						if depNameProps[pr.Name] {
							collectStrings(pr.Value, deps)
						}
					}
					if !deps[e.Provider] || !moduleSrcsInclude(m, filepath.Dir(p), e.HeaderPrefix) {
						continue
					}
					if addToListProp(m, "header_libs", e.HeaderLib) {
						changed = true
						rel, _ := filepath.Rel(outRoot, p)
						r.HeaderLibs = append(r.HeaderLibs, filepath.ToSlash(rel)+": "+m.Name()+" += header_libs "+e.HeaderLib+" (the target's "+e.Provider+" no longer re-exports "+e.Exported+")")
					}
				}
				if !changed {
					return
				}
				out, perr := parser.Print(file)
				if perr != nil {
					return
				}
				_ = os.WriteFile(p, out, 0o644)
			})
		}
	}
}
