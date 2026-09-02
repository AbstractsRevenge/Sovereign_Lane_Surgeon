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
	"sort"
	"strconv"
	"strings"

	parser "github.com/abstractsrevenge/sovereign_lane_surgeon/internal/blueprint/parser"
)

// aidlfloat.go — target-compat operation 10: a HAL floating on a newer AIDL version than the one
// it declares.
//
// WHY (observed 2026-09-02, cheetah `m droid superimage`, Build Capture run 151448Z):
// hardware/google/pixel/power-libperfmgr builds with `defaults: ["android.hardware.power-ndk_shared"]`,
// a Soong defaults module in hardware/interfaces/power that always names the interface's newest
// version (`power_version + "-ndk"`: V6 at android-15, V7 at android-17). The HAL's own support
// tables know V6's four SessionModes and its `static_assert` refuses V7's five — Google's guard
// telling us the HAL was written for V6, which its vintf fragment states outright
// (`<name>android.hardware.power</name> <version>6</version>`). Google's public tree has not moved
// the HAL to V7, so there is no upstream file to take. The faithful build is the one the source tag
// made: pin the module to the version its fragment declares. Rule, driven by the tree: for a module
// with a vintf fragment declaring X at version N, every module in the same Blueprint that floats on
// X-ndk_shared / X-ndk_static is re-pinned to X-V<N>-ndk (shared_libs / static_libs) when the
// target's defaults resolve to a version above N and the target still ships V<N>. Interfaces the
// fragment does not declare (graphics.common under a lights HAL) keep floating: no evidence.

var aidlFloatSuffixes = []string{"-ndk_shared", "-ndk_static"}

// evalBpStrings evaluates an expression to its string leaves, resolving file-level variables and
// "+" concatenations (`power_version + "-ndk"`), and descending into maps (target: { linux: {…} }).
func evalBpStrings(e parser.Expression, vars map[string]parser.Expression, out *[]string) {
	switch v := e.(type) {
	case *parser.String:
		*out = append(*out, v.Value)
	case *parser.Variable:
		if val, ok := vars[v.Name]; ok {
			evalBpStrings(val, vars, out)
		}
	case *parser.Operator:
		if v.Operator == '+' {
			var l, r []string
			evalBpStrings(v.Args[0], vars, &l)
			evalBpStrings(v.Args[1], vars, &r)
			if len(l) == 1 && len(r) == 1 {
				*out = append(*out, l[0]+r[0])
				return
			}
			*out = append(*out, l...)
			*out = append(*out, r...)
		}
	case *parser.List:
		for _, it := range v.Values {
			evalBpStrings(it, vars, out)
		}
	case *parser.Map:
		for _, p := range v.Properties {
			evalBpStrings(p.Value, vars, out)
		}
	case *parser.Select:
		for _, c := range v.Cases {
			if c != nil && c.Value != nil {
				evalBpStrings(c.Value, vars, out)
			}
		}
	}
}

// targetFloatVersion returns the version the target's defaults module named def (e.g.
// "android.hardware.power-ndk_shared") resolves to, by evaluating its lib lists, and whether the
// interface's aidl_interface in the same tree ships version n.
func targetFloatVersion(outRoot, iface, def string, n int) (resolved int, hasN bool) {
	for _, root := range aidlInterfaceRoots {
		walkFiles(filepath.Join(outRoot, filepath.FromSlash(root)), isBlueprint, func(p string) {
			if resolved != 0 && hasN {
				return
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return
			}
			file, errs := parser.Parse(p, bytes.NewReader(b))
			if len(errs) > 0 {
				return
			}
			vars := map[string]parser.Expression{}
			for _, d := range file.Defs {
				if a, ok := d.(*parser.Assignment); ok {
					vars[a.Name] = a.Value
				}
			}
			for _, d := range file.Defs {
				m, ok := d.(*parser.Module)
				if !ok {
					continue
				}
				if m.Name() == def {
					var libs []string
					for _, pr := range m.Properties {
						evalBpStrings(pr.Value, vars, &libs)
					}
					for _, l := range libs {
						if pin, ok := parseAidlPin(l); ok && pin.Iface == iface {
							resolved = pin.Ver
						}
					}
				}
				if m.Type == "aidl_interface" && m.Name() == iface {
					for _, pr := range m.Properties {
						if pr.Name == "versions" || pr.Name == "versions_with_info" {
							if lst, ok := pr.Value.(*parser.List); ok && len(lst.Values) >= n {
								hasN = true
							}
						}
					}
				}
			}
		})
	}
	return resolved, hasN
}

// vintfDeclaredVersions parses a vintf fragment for <hal> entries: <name>X</name> … <version>N</version>.
func vintfDeclaredVersions(xml []byte) map[string]int {
	out := map[string]int{}
	s := string(xml)
	for {
		i := strings.Index(s, "<hal")
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], "</hal>")
		if j < 0 {
			break
		}
		block := s[i : i+j]
		s = s[i+j:]
		name := between(block, "<name>", "</name>")
		ver := between(block, "<version>", "</version>")
		if name == "" || ver == "" {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(ver)); err == nil {
			out[strings.TrimSpace(name)] = n
		}
	}
	return out
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	rest := s[i+len(a):]
	j := strings.Index(rest, b)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// removeFromListProp drops val from the module's list property; removes the property if emptied.
func removeFromListProp(m *parser.Module, prop, val string) bool {
	for i, pr := range m.Properties {
		if pr.Name != prop {
			continue
		}
		lst, ok := pr.Value.(*parser.List)
		if !ok {
			return false
		}
		kept := lst.Values[:0]
		removed := false
		for _, v := range lst.Values {
			if s, ok := v.(*parser.String); ok && s.Value == val {
				removed = true
				continue
			}
			kept = append(kept, v)
		}
		lst.Values = kept
		if removed && len(kept) == 0 {
			m.Properties = append(m.Properties[:i], m.Properties[i+1:]...)
		}
		return removed
	}
	return false
}

func pinFloatingAidlDefaults(outRoot string, roots []string, r *compatReport) {
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
			// 1. what the file's HALs declare
			declared := map[string]int{}
			for _, d := range file.Defs {
				m, ok := d.(*parser.Module)
				if !ok {
					continue
				}
				for _, pr := range m.Properties {
					if pr.Name != "vintf_fragments" {
						continue
					}
					frags := map[string]bool{}
					collectStrings(pr.Value, frags)
					for f := range frags {
						if x, xerr := os.ReadFile(filepath.Join(filepath.Dir(p), filepath.FromSlash(f))); xerr == nil {
							for iface, n := range vintfDeclaredVersions(x) {
								declared[iface] = n
							}
						}
					}
				}
			}
			if len(declared) == 0 {
				return
			}
			// 2. pin every module in the file floating on a declared interface
			changed := false
			var lines []string
			for _, d := range file.Defs {
				m, ok := d.(*parser.Module)
				if !ok {
					continue
				}
				defs := map[string]bool{}
				for _, pr := range m.Properties {
					if pr.Name == "defaults" {
						collectStrings(pr.Value, defs)
					}
				}
				for def := range defs {
					for _, suf := range aidlFloatSuffixes {
						if !strings.HasSuffix(def, suf) {
							continue
						}
						iface := strings.TrimSuffix(def, suf)
						n, ok := declared[iface]
						if !ok {
							continue
						}
						resolved, hasN := targetFloatVersion(outRoot, iface, def, n)
						if resolved <= n || !hasN {
							continue
						}
						prop := "shared_libs"
						if suf == "-ndk_static" {
							prop = "static_libs"
						}
						pin := iface + "-V" + strconv.Itoa(n) + "-ndk"
						if removeFromListProp(m, "defaults", def) {
							addToListProp(m, prop, pin)
							changed = true
							lines = append(lines, m.Name()+": "+def+" (V"+strconv.Itoa(resolved)+" on the target) → "+prop+" "+pin+" (the fragment declares V"+strconv.Itoa(n)+")")
						}
					}
				}
			}
			if !changed {
				return
			}
			out, perr := parser.Print(file)
			if perr != nil || os.WriteFile(p, out, 0o644) != nil {
				return
			}
			rel, _ := filepath.Rel(outRoot, p)
			sort.Strings(lines)
			for _, l := range lines {
				r.AidlPins = append(r.AidlPins, filepath.ToSlash(rel)+": "+l)
			}
		})
	}
}
