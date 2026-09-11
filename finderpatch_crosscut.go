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
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// finderpatch_crosscut.go — §23.1 step 4 (part C): the CROSS-CUTTING edit. When a new lane is
// added, every EXISTING other-lane func (isOtherLaneBp, isOtherLaneBpFor<X>) must also drop the
// new lane's dir — otherwise a holo/nexusm build would load the new lane's bps and collide. This
// appends `|| strings.HasSuffix(comp, "-<lane>")` to each such func's OR-chain condition via
// go/ast location (HARD RULE 3 — no regex), spliced after the last existing HasSuffix(comp,...)
// operand with the operand's own indentation, and re-parsed to prove the result compiles.

// isHasSuffixCompCall reports whether n is `strings.HasSuffix(comp, <lit>)`.
func isHasSuffixCompCall(n ast.Node) bool {
	ce, ok := n.(*ast.CallExpr)
	if !ok || len(ce.Args) != 2 {
		return false
	}
	sel, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "HasSuffix" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "strings" {
		return false
	}
	arg0, ok := ce.Args[0].(*ast.Ident)
	return ok && arg0.Name == "comp"
}

// appendSuffixToOtherLaneFunc appends `|| strings.HasSuffix(comp, newSuffix)` to the OR-chain
// inside funcName, after the last existing strings.HasSuffix(comp, ...) operand. Idempotent:
// no-op if newSuffix is already referenced in the func body.
func appendSuffixToOtherLaneFunc(src []byte, funcName, newSuffix string) (out []byte, changed bool, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, false, fmt.Errorf("parse: %w", err)
	}
	var fd *ast.FuncDecl
	for _, d := range f.Decls {
		if x, ok := d.(*ast.FuncDecl); ok && x.Name.Name == funcName {
			fd = x
			break
		}
	}
	if fd == nil {
		return nil, false, fmt.Errorf("func %q not found", funcName)
	}
	// Idempotency: newSuffix already in the func's source span?
	fnStart := fset.Position(fd.Pos()).Offset
	fnEnd := fset.Position(fd.End()).Offset
	if strings.Contains(string(src[fnStart:fnEnd]), fmt.Sprintf("%q", newSuffix)) {
		return src, false, nil
	}
	// Find the last strings.HasSuffix(comp, ...) call in the func (max End offset).
	var last *ast.CallExpr
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if isHasSuffixCompCall(n) {
			ce := n.(*ast.CallExpr)
			if last == nil || ce.End() > last.End() {
				last = ce
			}
		}
		return true
	})
	if last == nil {
		// The first lane in a tree has no siblings, so its generated function intentionally ends
		// in a bare `return false` with no suffix loop. Adding the second lane is the first time
		// that function needs a predicate; seed the loop before its final false return. Later
		// lanes take the normal OR-chain path above.
		var finalFalse *ast.ReturnStmt
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			rs, ok := n.(*ast.ReturnStmt)
			if !ok || len(rs.Results) != 1 {
				return true
			}
			id, ok := rs.Results[0].(*ast.Ident)
			if ok && id.Name == "false" && (finalFalse == nil || rs.Pos() > finalFalse.Pos()) {
				finalFalse = rs
			}
			return true
		})
		if finalFalse == nil {
			return nil, false, fmt.Errorf("no suffix chain or final false return in func %q", funcName)
		}
		off := fset.Position(finalFalse.Pos()).Offset
		ins := []byte(fmt.Sprintf("for _, comp := range strings.Split(bp, \"/\") {\n\t\tif strings.HasSuffix(comp, %q) {\n\t\t\treturn true\n\t\t}\n\t}\n\t", newSuffix))
		out = append(append(append([]byte{}, src[:off]...), ins...), src[off:]...)
		if _, perr := parser.ParseFile(token.NewFileSet(), "", out, 0); perr != nil {
			return nil, false, fmt.Errorf("post-splice reparse failed: %w", perr)
		}
		return out, true, nil
	}
	pos := fset.Position(last.Pos())
	indent := strings.Repeat("\t", pos.Column-1) // operand's own indentation (tabs = 1 col each)
	off := fset.Position(last.End()).Offset
	ins := []byte(fmt.Sprintf(" ||\n%sstrings.HasSuffix(comp, %q)", indent, newSuffix))
	out = append(append(append([]byte{}, src[:off]...), ins...), src[off:]...)
	if _, perr := parser.ParseFile(token.NewFileSet(), "", out, 0); perr != nil {
		return nil, false, fmt.Errorf("post-splice reparse failed: %w", perr)
	}
	return out, true, nil
}

// patchExistingOtherLaneFuncs appends the new lane's suffix to every existing isOtherLaneBp*
// func (except the new lane's own). Returns the names of funcs actually changed.
func patchExistingOtherLaneFuncs(src []byte, newLane, newCamel string) (out []byte, changedFuncs []string, err error) {
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, "", src, parser.ParseComments)
	if perr != nil {
		return nil, nil, fmt.Errorf("parse: %w", perr)
	}
	ownFunc := "isOtherLaneBpFor" + newCamel
	var names []string
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok &&
			strings.HasPrefix(fd.Name.Name, "isOtherLaneBp") && fd.Name.Name != ownFunc {
			names = append(names, fd.Name.Name)
		}
	}
	out = src
	for _, n := range names {
		o, changed, e := appendSuffixToOtherLaneFunc(out, n, "-"+newLane)
		if e != nil {
			return nil, nil, e
		}
		if changed {
			changedFuncs = append(changedFuncs, n)
		}
		out = o
	}
	return out, changedFuncs, nil
}
