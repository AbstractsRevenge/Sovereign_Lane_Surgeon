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
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// uninstall.go — reverses a `create`+`apply`. The interference (2026-07-12) showed a seed writes
// to SHARED dirs (goldfish/cuttlefish products + AndroidProducts.mk) beyond the lane-specific dirs,
// and manual cleanup missed them. This removes ALL of it surgically (multi-lane-safe): lane dirs +
// device dirs + shared products + the lane's AndroidProducts.mk lines + the lane's soong additions
// (inverse AST — no snapshot dependency). Then it reminds to clear sibling-lane finder caches.

// removeStringElem removes elem from the first []string{...} inside funcName (inverse of
// appendStringElem). Byte-splices out `elem` and its adjacent comma. Idempotent.
func removeStringElem(src []byte, funcName, elem string) (out []byte, changed bool, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, false, err
	}
	var lit *ast.CompositeLit
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != funcName || fd.Body == nil {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if lit != nil {
				return false
			}
			if cl, ok := n.(*ast.CompositeLit); ok {
				if at, ok := cl.Type.(*ast.ArrayType); ok {
					if id, ok := at.Elt.(*ast.Ident); ok && id.Name == "string" {
						lit = cl
						return false
					}
				}
			}
			return true
		})
		break
	}
	if lit == nil {
		return src, false, nil
	}
	for i, e := range lit.Elts {
		bl, ok := e.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			continue
		}
		if v, uerr := strconv.Unquote(bl.Value); uerr != nil || v != elem {
			continue
		}
		start := fset.Position(bl.Pos()).Offset
		end := fset.Position(bl.End()).Offset
		if i > 0 { // eat the preceding ", " (and any newline/indent)
			prevEnd := fset.Position(lit.Elts[i-1].End()).Offset
			start = prevEnd
		} else if len(lit.Elts) > 1 { // first elem: eat the following ", "
			nextStart := fset.Position(lit.Elts[i+1].Pos()).Offset
			end = nextStart
		}
		out = append(append([]byte{}, src[:start]...), src[end:]...)
		if _, perr := parser.ParseFile(token.NewFileSet(), "", out, 0); perr != nil {
			return nil, false, fmt.Errorf("post-remove reparse failed: %w", perr)
		}
		return out, true, nil
	}
	return src, false, nil
}

// removeDeclByName splices out the top-level func/type declaration named `name` (with its doc
// comment + trailing blank line). Re-parses to verify. No-op if absent.
func removeDeclByName(src []byte, name string) (out []byte, changed bool, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, false, err
	}
	for _, d := range f.Decls {
		var declName string
		var doc *ast.CommentGroup
		switch x := d.(type) {
		case *ast.FuncDecl:
			declName, doc = x.Name.Name, x.Doc
		case *ast.GenDecl:
			if len(x.Specs) == 1 {
				if ts, ok := x.Specs[0].(*ast.TypeSpec); ok {
					declName, doc = ts.Name.Name, x.Doc
				}
			}
		}
		if declName != name {
			continue
		}
		startPos := d.Pos()
		if doc != nil {
			startPos = doc.Pos()
		}
		start := lineStartOffset(fset, startPos)
		end := fset.Position(d.End()).Offset
		// eat trailing newlines up to the next non-blank
		for end < len(src) && (src[end] == '\n') {
			end++
		}
		out = append(append([]byte{}, src[:start]...), src[end:]...)
		if _, perr := parser.ParseFile(token.NewFileSet(), "", out, 0); perr != nil {
			return nil, false, fmt.Errorf("post-remove reparse failed: %w", perr)
		}
		return out, true, nil
	}
	return src, false, nil
}

// removePipelineCall removes the `if is<Camel>Lane(config) { ... }` block from the finder pipeline.
func removePipelineCall(src []byte, camel string) (out []byte, changed bool, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, false, err
	}
	marker := "is" + camel + "Lane"
	var target *ast.IfStmt
	ast.Inspect(f, func(n ast.Node) bool {
		if target != nil {
			return false
		}
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		if ce, ok := ifs.Cond.(*ast.CallExpr); ok {
			if id, ok := ce.Fun.(*ast.Ident); ok && id.Name == marker {
				target = ifs
				return false
			}
		}
		return true
	})
	if target == nil {
		return src, false, nil
	}
	start := lineStartOffset(fset, target.Pos())
	end := fset.Position(target.End()).Offset
	for end < len(src) && src[end] == '\n' {
		end++
	}
	out = append(append([]byte{}, src[:start]...), src[end:]...)
	if _, perr := parser.ParseFile(token.NewFileSet(), "", out, 0); perr != nil {
		return nil, false, fmt.Errorf("post-remove reparse failed: %w", perr)
	}
	return out, true, nil
}

// removeSwitchCase removes a string case from the switch inside funcName (inverse of
// appendFrameworkResCase). Splices out the case literal + its adjacent comma.
func removeSwitchCase(src []byte, funcName, caseVal string) (out []byte, changed bool, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, false, err
	}
	var fd *ast.FuncDecl
	for _, d := range f.Decls {
		if x, ok := d.(*ast.FuncDecl); ok && x.Name.Name == funcName {
			fd = x
			break
		}
	}
	if fd == nil {
		return src, false, nil
	}
	var cc *ast.CaseClause
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CaseClause); ok && cc == nil && len(c.List) > 0 {
			cc = c
		}
		return true
	})
	if cc == nil {
		return src, false, nil
	}
	for i, e := range cc.List {
		bl, ok := e.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			continue
		}
		if v, uerr := strconv.Unquote(bl.Value); uerr != nil || v != caseVal {
			continue
		}
		start := fset.Position(bl.Pos()).Offset
		end := fset.Position(bl.End()).Offset
		if i > 0 {
			start = fset.Position(cc.List[i-1].End()).Offset
		} else if len(cc.List) > 1 {
			end = fset.Position(cc.List[i+1].Pos()).Offset
		}
		out = append(append([]byte{}, src[:start]...), src[end:]...)
		if _, perr := parser.ParseFile(token.NewFileSet(), "", out, 0); perr != nil {
			return nil, false, fmt.Errorf("post-remove reparse failed: %w", perr)
		}
		return out, true, nil
	}
	return src, false, nil
}

// removeSuffixFromOtherLaneFuncs removes `|| strings.HasSuffix(comp, "-<lane>")` from every existing
// isOtherLaneBp* func (inverse of the cross-cut). Simple text removal of the exact spliced form,
// re-parsed to verify.
func removeSuffixFromOtherLaneFuncs(src []byte, lane string) (out []byte, changed bool, err error) {
	s := string(src)
	// The cross-cut spliced ` ||` + newline + the operand's own indentation + the HasSuffix call
	// (see appendSuffixToOtherLaneFunc). Match any tab-indent 2–4 deep to be robust.
	suffixCall := fmt.Sprintf("strings.HasSuffix(comp, \"-%s\")", lane)
	found := false
	for _, indent := range []string{"\t\t\t\t", "\t\t\t", "\t\t"} {
		pat := " ||\n" + indent + suffixCall
		if strings.Contains(s, pat) {
			s = strings.ReplaceAll(s, pat, "")
			found = true
		}
	}
	if !found {
		return src, false, nil
	}
	out = []byte(s)
	if _, perr := parser.ParseFile(token.NewFileSet(), "", out, 0); perr != nil {
		return nil, false, fmt.Errorf("post-remove reparse failed: %w", perr)
	}
	return out, true, nil
}

// applyGoEdit reads outRoot/rel, runs each edit fn in sequence, writes back if changed.
func applyGoEdit(outRoot, rel string, edits ...func([]byte) ([]byte, bool, error)) {
	p := filepath.Join(outRoot, rel)
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	any := false
	for _, ed := range edits {
		out, ch, eerr := ed(b)
		if eerr != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", rel, eerr)
			return
		}
		if ch {
			b, any = out, true
		}
	}
	if any {
		os.WriteFile(p, b, 0o644)
		fmt.Printf("  - reverted %s\n", rel)
	}
}

// cleanAndroidProducts surgically removes the lane's product lines (multi-lane-safe — leaves other
// lanes' entries intact, unlike restoring the .sld-bak).
func cleanAndroidProducts(outRoot, rel, lane string) {
	p := filepath.Join(outRoot, rel)
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	pats := []string{"_" + lane + ".mk", "_" + lane + ":", "_" + lane + "-"}
	var keep []string
	removed := 0
	for _, line := range strings.Split(string(b), "\n") {
		drop := false
		for _, pat := range pats {
			if strings.Contains(line, pat) {
				drop = true
			}
		}
		if drop {
			removed++
			continue
		}
		keep = append(keep, line)
	}
	if removed > 0 {
		os.WriteFile(p, []byte(strings.Join(keep, "\n")), 0o644)
		fmt.Printf("  - removed %d lane line(s) from %s\n", removed, rel)
	}
	// drop the surgeon's leftover backup (its purpose is served)
	os.Remove(p + ".sld-bak")
}

// uninstallLane reverses a create+apply for the lane. target selects how much:
//
//	"patches" — soong/make patches only. The lane TREES are left on disk, so the lane can be
//	            re-registered without re-cloning. Use when re-seeding (e.g. renaming a lane) —
//	            a full uninstall discards a multi-GB clone that takes minutes to rebuild and
//	            may hold hand-edits.
//	"lanes"   — lane trees + products only, leaving the patches in place.
//	"all"     — both (the historical behavior, and the default).
//
// NOTE the standing hazard between them: lane trees on disk with the patches REVERTED is fine
// (nothing routes to them), but trees on disk while a SIBLING lane's isOtherLaneBp still names
// this suffix is also fine — it is the reverse (trees present, this lane's suffix NOT registered
// in the siblings' predicates) that lets a sibling load these bps and collide on keep-name twins.
// "patches" leaves the cross-cut in place precisely so that gap never opens.
func uninstallLane(c LaneConfig, outRoot string, target string) int {
	if target == "" {
		target = "all"
	}
	doTrees := target == "all" || target == "lanes"
	doPatches := target == "all" || target == "patches"
	fmt.Printf("uninstall lane %q from %s (target=%s):\n", c.Name, outRoot, target)
	if doTrees {
		// 1. lane dirs + device dirs
		dirs := []string{"frameworks-" + c.Name, "packages-" + c.Name, "." + c.Name}
		devMatches, _ := filepath.Glob(filepath.Join(outRoot, "device", "google", "*-"+c.Name))
		for _, m := range devMatches {
			dirs = append(dirs, strings.TrimPrefix(m, outRoot+string(os.PathSeparator)))
		}
		for _, d := range dirs {
			abs := filepath.Join(outRoot, d)
			if _, err := os.Stat(abs); err == nil {
				os.RemoveAll(abs)
				fmt.Printf("  - removed %s/\n", d)
			}
		}
		// 2. shared goldfish + cuttlefish products
		for _, p := range []string{
			"device/generic/goldfish/64bitonly/product/sdk_phone64_" + c.Name + ".mk",
			"device/generic/goldfish/64bitonly/product/sdk_tablet_" + c.Name + ".mk",
			"device/google/cuttlefish/vsoc_x86_64/phone/aosp_cf_" + c.Name + ".mk",
			"device/google/cuttlefish/vsoc_x86_64/phone/aosp_cf_tablet_" + c.Name + ".mk",
		} {
			abs := filepath.Join(outRoot, p)
			if _, err := os.Stat(abs); err == nil {
				os.Remove(abs)
				fmt.Printf("  - removed %s\n", p)
			}
		}
		// 3. shared AndroidProducts.mk (surgical, multi-lane-safe). Product REGISTRATION, so it
		// belongs with the trees: leaving it while the products are gone would register a
		// nonexistent product; removing it while the products remain would orphan them.
		cleanAndroidProducts(outRoot, "device/generic/goldfish/AndroidProducts.mk", c.Name)
		cleanAndroidProducts(outRoot, "device/google/cuttlefish/AndroidProducts.mk", c.Name)
	} else {
		fmt.Printf("  = lane trees KEPT (frameworks-%s/, packages-%s/, .%s/, products)\n", c.Name, c.Name, c.Name)
	}
	if !doPatches {
		fmt.Printf("  = soong/make patches KEPT — the lane stays registered.\n")
		fmt.Println("uninstall complete.")
		return 0
	}
	// 4. soong patches (inverse AST)
	camel := c.CamelCase
	applyGoEdit(outRoot, "build/soong/java/aar.go",
		func(b []byte) ([]byte, bool, error) { return removeStringElem(b, "isLaneLunch", "_"+c.Name) },
		func(b []byte) ([]byte, bool, error) {
			return removeSwitchCase(b, "isFrameworkResClassName", "framework-res-"+c.Name)
		})
	applyGoEdit(outRoot, "build/soong/android/visibility.go",
		func(b []byte) ([]byte, bool, error) { return removeStringElem(b, "laneCanonicalPkgs", "-"+c.Name) })
	applyGoEdit(outRoot, "build/soong/ui/build/finder.go",
		func(b []byte) ([]byte, bool, error) { return removeDeclByName(b, "is"+camel+"Lane") },
		func(b []byte) ([]byte, bool, error) { return removeDeclByName(b, c.Name+"BpRouteManifest") },
		func(b []byte) ([]byte, bool, error) { return removeDeclByName(b, "load"+camel+"BpRouteManifest") },
		func(b []byte) ([]byte, bool, error) { return removeDeclByName(b, "apply"+camel+"BpRoutes") },
		func(b []byte) ([]byte, bool, error) { return removeDeclByName(b, "isOtherLaneBpFor"+camel) },
		func(b []byte) ([]byte, bool, error) { return removeDeclByName(b, c.Name+"StockParallel") },
		func(b []byte) ([]byte, bool, error) { return removePipelineCall(b, camel) },
		func(b []byte) ([]byte, bool, error) { return removeSuffixFromOtherLaneFuncs(b, c.Name) })
	// 5. stock license-metadata patch (lane-INDEPENDENT) — reverse to byte-identical stock.
	//    NOTE: shared across lanes; if other lanes remain, re-run their `apply` to restore it.
	applyGoEdit(outRoot, compatibilityMkRel,
		func(b []byte) ([]byte, bool, error) { return unpatchCompatibilityMk(b) })
	// 6. staging/snapshot cruft
	os.RemoveAll(filepath.Join(outRoot, ".sld-staging"))
	fmt.Printf("\n⚠ Finally, clear the finder cache of any OTHER lane's out-dir that built during this\n  lane's lifetime, so it re-scans clean:  rm -rf out-*/*/.module_paths\n")
	fmt.Printf("uninstall complete.\n")
	return 0
}

func cmdUninstall(args []string) int {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	out := fs.String("out", "", "AOSP root to reverse the lane from")
	name := fs.String("name", "", "lane name to uninstall")
	target := fs.String("target", "all", "what to remove: all | patches | lanes.\n"+
		"  all     — trees + products + soong/make patches (full reversal)\n"+
		"  patches — soong/make patches ONLY; the lane trees stay on disk (re-seed without re-cloning)\n"+
		"  lanes   — lane trees + products ONLY; the patches stay registered")
	_ = fs.Parse(args)
	if *out == "" || *name == "" {
		fmt.Fprintln(os.Stderr, "uninstall: -name <lane> and -out <aosp-root> are required")
		return 2
	}
	switch *target {
	case "all", "patches", "lanes":
	default:
		fmt.Fprintf(os.Stderr, "uninstall: -target must be all|patches|lanes (got %q)\n", *target)
		return 2
	}
	return uninstallLane(deriveLane(*name, true, nil, false, false, ""), *out, *target)
}
