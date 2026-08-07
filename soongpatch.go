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
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// soongpatch.go — §23.1 step 4 (part A): register a new sovereign lane in the Soong lane-routing
// layer. The two CENTRAL registration points are single-line []string slices that everything
// else keys off:
//
//	build/soong/java/aar.go       isLaneLunch()       — append "_<lane>"; auto-enrolls all 5
//	                                                    shouldSuppressStock* framework suppressors
//	build/soong/android/visibility.go laneCanonicalPkgs() — append "-<lane>"; lane→stock pkg mapping
//
// Both are patched by locating the []string composite literal via go/ast (HARD RULE 3 — never a
// regex on Go source) and splicing one element after the last, re-parsing to prove the result
// still compiles. The finder.go additive per-lane funcs + pipeline call + cross-cutting other-lane
// suffix edits are a separate part (finderpatch.go). The route manifest is emitted here (part B).

// appendStringElem splices newElem (raw; this func quotes it) into the first []string{...}
// composite literal inside the top-level function funcName of src. AST-located, byte-spliced
// after the last existing element so ONLY that line changes (go/printer would reflow the whole
// file). Re-parses the result to guarantee it still compiles. Idempotent: if newElem is already
// present, returns (src, false, nil). Errors if the slice is multi-line (inline-only, matching
// the two real targets).
func appendStringElem(src []byte, funcName, newElem string) (out []byte, changed bool, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, false, fmt.Errorf("parse: %w", err)
	}
	var lit *ast.CompositeLit
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != funcName || fd.Body == nil {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if lit != nil {
				return false
			}
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			at, ok := cl.Type.(*ast.ArrayType)
			if !ok {
				return true
			}
			if id, ok := at.Elt.(*ast.Ident); ok && id.Name == "string" {
				lit = cl
				return false
			}
			return true
		})
		break
	}
	if lit == nil {
		return nil, false, fmt.Errorf("no []string{...} literal found in func %q", funcName)
	}
	if len(lit.Elts) == 0 {
		return nil, false, fmt.Errorf("func %q has an empty []string literal", funcName)
	}
	for _, e := range lit.Elts {
		bl, ok := e.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			continue
		}
		if v, uerr := strconv.Unquote(bl.Value); uerr == nil && v == newElem {
			return src, false, nil // already registered — idempotent
		}
	}
	last := lit.Elts[len(lit.Elts)-1]
	if fset.Position(lit.Rbrace).Line != fset.Position(last.Pos()).Line {
		return nil, false, fmt.Errorf("func %q slice is multi-line; inline-only appender", funcName)
	}
	off := fset.Position(last.End()).Offset
	ins := []byte(fmt.Sprintf(", %q", newElem))
	out = append(append(append([]byte{}, src[:off]...), ins...), src[off:]...)
	if _, perr := parser.ParseFile(token.NewFileSet(), "", out, 0); perr != nil {
		return nil, false, fmt.Errorf("post-splice reparse failed (would corrupt source): %w", perr)
	}
	return out, true, nil
}

// PatchIsLaneLunch registers the lane in aar.go's isLaneLunch suffix set (auto-enrolls the 5
// shouldSuppressStock* framework suppressors — a single-slot enrollment, by design).
func PatchIsLaneLunch(src []byte, lane string) ([]byte, bool, error) {
	return appendStringElem(src, "isLaneLunch", "_"+lane)
}

// PatchLaneCanonicalPkgs registers the lane's dir suffix in visibility.go's laneCanonicalPkgs
// (the lane→stock package-dir mapping used for visibility resolution).
func PatchLaneCanonicalPkgs(src []byte, lane string) ([]byte, bool, error) {
	return appendStringElem(src, "laneCanonicalPkgs", "-"+lane)
}

// appendFrameworkResCase adds "framework-res-<lane>" to the case list of aar.go's
// isFrameworkResClassName switch (RENAME model — so the renamed lane framework-res gets Soong's
// framework-res special-casing). AST-located, byte-spliced, re-parsed. Idempotent.
func appendFrameworkResCase(src []byte, lane string) (out []byte, changed bool, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, false, fmt.Errorf("parse: %w", err)
	}
	var fd *ast.FuncDecl
	for _, d := range f.Decls {
		if x, ok := d.(*ast.FuncDecl); ok && x.Name.Name == "isFrameworkResClassName" {
			fd = x
			break
		}
	}
	if fd == nil {
		return nil, false, fmt.Errorf("func isFrameworkResClassName not found")
	}
	newLit := "framework-res-" + lane
	fnStart := fset.Position(fd.Pos()).Offset
	fnEnd := fset.Position(fd.End()).Offset
	if strings.Contains(string(src[fnStart:fnEnd]), fmt.Sprintf("%q", newLit)) {
		return src, false, nil // idempotent
	}
	var lastLit *ast.BasicLit
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if cc, ok := n.(*ast.CaseClause); ok {
			for _, e := range cc.List {
				if bl, ok := e.(*ast.BasicLit); ok && bl.Kind == token.STRING {
					lastLit = bl
				}
			}
		}
		return true
	})
	if lastLit == nil {
		return nil, false, fmt.Errorf("no string case in isFrameworkResClassName")
	}
	pos := fset.Position(lastLit.Pos())
	indent := strings.Repeat("\t", pos.Column-1)
	off := fset.Position(lastLit.End()).Offset
	ins := []byte(fmt.Sprintf(",\n%s%q", indent, newLit))
	out = append(append(append([]byte{}, src[:off]...), ins...), src[off:]...)
	if _, perr := parser.ParseFile(token.NewFileSet(), "", out, 0); perr != nil {
		return nil, false, fmt.Errorf("post-splice reparse failed: %w", perr)
	}
	return out, true, nil
}

// routeManifest is the lean BP route-curation manifest (the nexusm shape — the finder's
// suffix-rule isOtherLaneBpFor<Lane> does the bulk cross-lane drop; this carries only the
// specific soong_namespace bps the suffix rule misses).
type routeManifest struct {
	SchemaVersion             string   `json:"schema_version"`
	Lane                      string   `json:"lane"`
	Description               string   `json:"description"`
	DroppedNamespaceDeclPaths []string `json:"dropped_namespace_decl_paths"`
	AddedNamespaceDeclPaths   []string `json:"added_namespace_decl_paths"`
}

// knownStockDangles are non-lane stock bp whose modules dangle in a typical AOSP checkout
// (deprecated/partial-checkout/test-tool deps). Dropping them is how a complete lane goes
// ZERO-FLAG — it accommodates the danglers SURGICALLY (vs ALLOW_MISSING_DEPENDENCIES, which
// blanket-absorbs ALL missing deps and would mask REAL lane errors). Mirrors the holo lane's
// manifest. Seeded only if present under -out (checkout-specific ones are simply absent).
var knownStockDangles = []string{
	"bootable/deprecated-ota/Android.bp",             // updater: kati exe links recovery-variant soong libs
	"device/google/cuttlefish/tests/Android.bp",      // cuttlefish test tree
	"external/aws-sdk-java-v2/Android.bp",            // cloud host-tool deps (partial checkout)
	"external/google-cloud-java/java-kms/Android.bp", // cloud-KMS host-tool deps (partial checkout)
	"test/catbox/Android.bp",
	"test/cts-root/tools/Android.bp",
	"test/mts/tools/Android.bp",
	"test/vts-testcase/hal/usb/Android.bp",
	"test/vts/tools/Android.bp",
	"tools/platform-compat/javatest/com/android/Android.bp",
}

// emitRouteManifest renders the .<lane>/<lane>_bp_route_manifest.json seed. dropped_namespace_decl_paths
// is pre-populated with whichever knownStockDangles exist under outRoot — so the lane builds ZERO-FLAG
// out of the box. Add more from build evidence (`doctor`) as they surface.
func emitRouteManifest(lane, camel, outRoot string) ([]byte, error) {
	drops := []string{}
	for _, p := range knownStockDangles {
		if outRoot != "" {
			if _, err := os.Stat(filepath.Join(outRoot, p)); err != nil {
				continue // not present in this checkout — skip
			}
		}
		drops = append(drops, p)
	}
	m := routeManifest{
		SchemaVersion: "1.0",
		Lane:          lane,
		Description: fmt.Sprintf("BP route curation for the %s sovereign lane. The finder's "+
			"isOtherLaneBpFor%s suffix rule handles the bulk cross-lane drop; this manifest carries (a) the "+
			"specific soong_namespace bps the suffix rule misses, and (b) seeded known-dangling stock bp "+
			"(deprecated-ota/cloud-KMS/test-infra) so the lane builds WITHOUT ALLOW_MISSING_DEPENDENCIES — "+
			"surgical dangler-drop, not blanket flag. Add more from `doctor`/build evidence as they surface.", lane, camel),
		DroppedNamespaceDeclPaths: drops,
		AddedNamespaceDeclPaths:   []string{},
	}
	return json.MarshalIndent(m, "", "  ")
}
