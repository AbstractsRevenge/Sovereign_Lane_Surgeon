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
	_ "embed"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	parser "github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/internal/blueprint/parser"
	"github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/internal/bpflags"
)

// werror.go — target-compat operation 9: -Werror under a newer toolchain.
//
// WHY (observed 2026-09-02, cheetah `m droid superimage`, Build Capture runs 150226Z and 150818Z):
// hardware/google/pixel/pixelstats/WaterEventReporter.cpp fails with two `unused variable`
// diagnostics promoted to errors. The file is byte-identical in Google's current upstream tree, so
// the code is not wrong for its tag — android-17's clang (clang-r584948) diagnoses what
// android-15's (clang-r536225) did not. Dropping the module's own "-Werror" is not enough:
// build/soong/cc/compiler.go injects -Werror into every compiling module outside
// WarningAllowedProjects (device/, vendor/ — build/soong/cc/config/global.go) that neither says
// "-Werror" nor "-Wno-error", and records "-Wno-error" as the sanctioned opt-out (UsingWnoError).
// So for mirrored compiling cc modules under a directory Soong does not allow warnings in, the
// operation removes "-Werror" and adds "-Wno-error" to cflags — only when the target's clang
// differs from the one the bundle's tag was built with (assets/aosp15_device.toolchain). Warnings
// still print; sources stay Google's byte for byte. Modules under device/ are left alone: Soong
// already allows their warnings.

//go:embed assets/aosp15_device.toolchain
var embeddedToolchainTable string

// bundleClang returns the clang version the bundle's sources were built with.
func bundleClang() string {
	for _, line := range strings.Split(embeddedToolchainTable, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "clang" {
			return f[1]
		}
	}
	return ""
}

// soongStringList reads a []string variable out of the target's global.go with go/ast.
func soongStringList(outRoot, name string) []string {
	f, err := goparser.ParseFile(token.NewFileSet(), filepath.Join(outRoot, filepath.FromSlash(soongGlobalCflagsRel)), nil, 0)
	if err != nil {
		return nil
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, id := range vs.Names {
			if id.Name != name || i >= len(vs.Values) {
				continue
			}
			if cl, ok := vs.Values[i].(*ast.CompositeLit); ok {
				for _, el := range cl.Elts {
					if lit, ok := el.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if s, uerr := strconv.Unquote(lit.Value); uerr == nil {
							out = append(out, s)
						}
					}
				}
			}
		}
		return true
	})
	return out
}

// targetClang reads ClangDefaultVersion out of the target's Soong config with go/ast.
func targetClang(outRoot string) string {
	f, err := goparser.ParseFile(token.NewFileSet(), filepath.Join(outRoot, filepath.FromSlash(soongGlobalCflagsRel)), nil, 0)
	if err != nil {
		return ""
	}
	ver := ""
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if name.Name == "ClangDefaultVersion" && i < len(vs.Values) {
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, uerr := strconv.Unquote(lit.Value); uerr == nil {
						ver = s
					}
				}
			}
		}
		return true
	})
	return ver
}

// compilingCcType reports whether a Soong module type compiles C/C++ sources of its own.
func compilingCcType(t string) bool {
	if !strings.HasPrefix(t, "cc_") {
		return false
	}
	for _, skip := range []string{"cc_prebuilt", "cc_library_headers", "cc_genrule", "cc_aconfig"} {
		if strings.HasPrefix(t, skip) {
			return false
		}
	}
	return true
}

// warningsAllowedDir mirrors Soong's warningsAreAllowed: the module dir starts with an entry of
// WarningAllowedProjects.
func warningsAllowedDir(rel string, allowed []string) bool {
	for _, a := range allowed {
		if strings.HasPrefix(rel, a) {
			return true
		}
	}
	return false
}

// relaxWerrorBp removes "-Werror" from every compiling module and adds "-Wno-error" to its cflags.
func relaxWerrorBp(content []byte) ([]byte, bool, error) {
	dropped, _, derr := bpflags.Drop(content, map[string]bool{"-Werror": true})
	if derr != nil {
		return content, false, derr
	}
	file, errs := parser.Parse("", bytes.NewReader(dropped))
	if len(errs) > 0 {
		return content, false, errs[0]
	}
	changed := false
	for _, def := range file.Defs {
		m, ok := def.(*parser.Module)
		if !ok || !compilingCcType(m.Type) {
			continue
		}
		if addToListProp(m, "cflags", "-Wno-error") {
			changed = true
		}
	}
	if !changed {
		return content, false, nil
	}
	out, err := parser.Print(file)
	if err != nil {
		return content, false, err
	}
	return out, true, nil
}

func dropWerrorUnderNewerClang(outRoot string, roots []string, r *compatReport) {
	src, tgt := bundleClang(), targetClang(outRoot)
	if src == "" || tgt == "" {
		r.Notes = append(r.Notes, "-Werror: could not compare toolchains (bundle "+src+", target "+tgt+") — left as mirrored")
		return
	}
	if src == tgt {
		return
	}
	allowed := soongStringList(outRoot, "WarningAllowedProjects")
	for _, root := range roots {
		if warningsAllowedDir(root+"/", allowed) {
			continue // Soong already lets these warn
		}
		walkFiles(filepath.Join(outRoot, filepath.FromSlash(root)), isBlueprint, func(p string) {
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return
			}
			out, ch, werr := relaxWerrorBp(b)
			if werr != nil || !ch {
				return
			}
			if os.WriteFile(p, out, 0o644) == nil {
				rel, _ := filepath.Rel(outRoot, p)
				r.WerrorFiles = append(r.WerrorFiles, filepath.ToSlash(rel))
			}
		})
	}
	if len(r.WerrorFiles) > 0 {
		r.Notes = append(r.Notes, "-Werror → -Wno-error in "+itoa(len(r.WerrorFiles))+" mirrored Blueprint(s) outside Soong's WarningAllowedProjects: the target compiles with "+tgt+", the bundle's tag with "+src+" — newer diagnostics warn instead of failing")
	}
}
