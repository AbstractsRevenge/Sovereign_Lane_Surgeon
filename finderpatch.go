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
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"text/template"
)

// finderpatch.go — §23.1 step 4 (part B): the finder.go per-lane routing funcs. Adds a new
// sovereign lane's SELF-CONTAINED routing block + the pipeline call that invokes it. The apply
// func is MODEL-AWARE (LaneConfig.KeepName): KEEP-NAME (Holo — the proven method) does per-file
// stock-parallel replacement (applyHoloBpRoutes shape); RENAME (NexusM) drops only other-lane
// dirs + manifest namespace decls (distinct names never collide). The funcs are rendered from
// templates, gofmt-normalized as a snippet, and spliced in via go/ast location (HARD RULE 3 —
// no regex on Go source), re-parsed to prove they still compile. Part C (finderpatch_crosscut.go)
// handles the cross-cutting edits that blind the EXISTING lanes to the new one.

type finderTmplData struct {
	Lane        string // "aurora"
	Camel       string // "Aurora" — CamelCase lib/module prefix (shared-infra additions: Aurora<name>)
	DirSuffix   string // "-aurora"
	DirPrefix   string // "AuroraM"-style identity-app dir/module prefix (rename model); "" for keep-name
	SuffixChain string // pre-built "strings.HasSuffix(comp, \"-holo\") || ..." (tabs handled by gofmt)
}

// finderSharedTmpl = the funcs common to BOTH lane models: detection, manifest struct + loader,
// and the other-lane-drop suffix rule. The apply func differs by model (below).
var finderSharedTmpl = template.Must(template.New("findershared").Parse(
	"// is{{.Camel}}Lane reports whether the current TARGET_PRODUCT is a {{.Lane}} sovereign lane\n" +
		"// variant (suffix _{{.Lane}}). Sibling to isHoloProduct/isNexusMNexus. Lane content lives in\n" +
		"// frameworks{{.DirSuffix}}/ + packages{{.DirSuffix}}/; a stock-fork with an independently-curated\n" +
		"// BP route manifest.\n" +
		"func is{{.Camel}}Lane(config Config) bool {\n" +
		"\ttargetProduct, err := config.TargetProductOrErr()\n" +
		"\tif err != nil {\n" +
		"\t\treturn false\n" +
		"\t}\n" +
		"\treturn strings.HasSuffix(targetProduct, \"_{{.Lane}}\")\n" +
		"}\n\n" +
		"// {{.Lane}}BpRouteManifest is the {{.Lane}}-lane BP route curation manifest (lean shape: the\n" +
		"// suffix rule isOtherLaneBpFor{{.Camel}} does the bulk cross-lane drop; this carries only the\n" +
		"// specific soong_namespace bps the suffix rule misses).\n" +
		"type {{.Lane}}BpRouteManifest struct {\n" +
		"\tDroppedNamespaceDeclPaths []string `json:\"dropped_namespace_decl_paths\"`\n" +
		"\tAddedNamespaceDeclPaths   []string `json:\"added_namespace_decl_paths\"`\n" +
		"}\n\n" +
		"// load{{.Camel}}BpRouteManifest reads the {{.Lane}} lane's BP route manifest if available.\n" +
		"// Returns nil (no error) when absent — optional infrastructure, identity transform. Fatal only\n" +
		"// on JSON parse errors (a corrupted manifest is a real bug).\n" +
		"func load{{.Camel}}BpRouteManifest(ctx Context, config Config) *{{.Lane}}BpRouteManifest {\n" +
		"\tmanifestPaths := []string{\n" +
		"\t\tfilepath.Join(config.SoongOutDir(), \"{{.Lane}}_bp_route_manifest.json\"),\n" +
		"\t\tfilepath.Join(\".{{.Lane}}\", \"{{.Lane}}_bp_route_manifest.json\"),\n" +
		"\t}\n" +
		"\tvar data []byte\n" +
		"\tfor _, candidate := range manifestPaths {\n" +
		"\t\tbytes, err := os.ReadFile(candidate)\n" +
		"\t\tif err == nil {\n" +
		"\t\t\tdata = bytes\n" +
		"\t\t\tbreak\n" +
		"\t\t}\n" +
		"\t\tif !os.IsNotExist(err) {\n" +
		"\t\t\tctx.Fatalf(\"{{.Camel}} BP route manifest unreadable at %q: %v\", candidate, err)\n" +
		"\t\t}\n" +
		"\t}\n" +
		"\tif data == nil {\n" +
		"\t\treturn nil\n" +
		"\t}\n" +
		"\tvar manifest {{.Lane}}BpRouteManifest\n" +
		"\tif err := json.Unmarshal(data, &manifest); err != nil {\n" +
		"\t\tctx.Fatalf(\"{{.Camel}} BP route manifest invalid: %v\", err)\n" +
		"\t}\n" +
		"\treturn &manifest\n" +
		"}\n\n" +
		"// isOtherLaneBpFor{{.Camel}} reports whether a bp lives in a NON-{{.Lane}} lane directory. Lane\n" +
		"// sovereignty: a {{.Lane}} build must be UNAWARE of alien lanes, so their bp are not loaded. The\n" +
		"// {{.Lane}} lane's own dirs end in \"{{.DirSuffix}}\" and are never matched here. external/kotlinc-holo/\n" +
		"// is carved out (its name ends in -holo but it is the universal compiler toolchain, not lane content).\n" +
		"func isOtherLaneBpFor{{.Camel}}(bp string) bool {\n" +
		"\tif strings.HasPrefix(bp, \"external/kotlinc-holo/\") {\n" +
		"\t\treturn false\n" +
		"\t}\n" +
		"\tfor _, comp := range strings.Split(bp, \"/\") {\n" +
		"\t\tif {{.SuffixChain}} {\n" +
		"\t\t\treturn true\n" +
		"\t\t}\n" +
		"\t}\n" +
		"\treturn false\n" +
		"}\n"))

// finderRenameApplyTmpl — the RENAME / Model-A hybrid apply (NexusM style: keep-name framework-class,
// renamed identity apps). Unlike the finder-inert original (which only dropped other-lane dirs +
// manifest decls, leaving the lane's keep-name framework-class + identity-app-internal modules to
// collide with stock), this performs PER-FILE stock-parallel replacement with the identity-app
// de-prefix (packages-<lane>/apps/<Prefix>Foo → packages/apps/Foo) so a renamed app's stock parallel
// and its keep-name test/internal twins drop rather than colliding in kati's flat MODULE.TARGET
// namespace. It generates its lane-prefixed helpers and references the shared bpDeclaresNamespace.
var finderRenameApplyTmpl = template.Must(template.New("renameapply").Parse(`// apply{{.Camel}}BpRoutes is the {{.Lane}} lane's BP filter (RENAME / Model-A hybrid: keep-name
// framework-class, renamed identity apps). For every frameworks{{.DirSuffix}}/|packages{{.DirSuffix}}/ bp
// it KEEPS the lane bp and DROPS its stock parallel (existing{{.Camel}}StockParallel — tree-prefix strip
// PLUS the identity-app de-prefix packages{{.DirSuffix}}/apps/{{.DirPrefix}}Foo → packages/apps/Foo, so a
// renamed app's stock parallel and its keep-name test/internal twins drop rather than colliding in kati's
// flat MODULE.TARGET namespace). frameworks{{.DirSuffix}} namespace decls collapse to the global namespace
// (dropped) so bare-name framework-class deps resolve to the lane variants — except is{{.Camel}}OwnedNamespaceBp
// subtrees (identity-app boundary + pods) which stay namespaced. Foreign-lane bp + manifest namespace decls
// are dropped. Requires the shared bpDeclaresNamespace helper.
func apply{{.Camel}}BpRoutes(ctx Context, config Config, androidBps []string) []string {
	manifest := load{{.Camel}}BpRouteManifest(ctx, config)
	toDrop := map[string]bool{}
	if manifest != nil {
		for _, decl := range manifest.DroppedNamespaceDeclPaths {
			toDrop[decl] = true
		}
	}
	for _, bp := range androidBps {
		if isOtherLaneBpFor{{.Camel}}(bp) {
			toDrop[bp] = true
			continue
		}
		if !strings.HasPrefix(bp, "frameworks{{.DirSuffix}}/") && !strings.HasPrefix(bp, "packages{{.DirSuffix}}/") {
			continue
		}
		if bpDeclaresNamespace(bp) {
			if !is{{.Camel}}OwnedNamespaceBp(bp) {
				toDrop[bp] = true // collapse to root (framework-class AND packages{{.DirSuffix}} apps), Holo-style
			}
			continue
		}
		if mapped := existing{{.Camel}}StockParallel(bp); mapped != "" {
			toDrop[mapped] = true
		}
	}
	if len(toDrop) == 0 {
		return androidBps
	}
	filtered := make([]string, 0, len(androidBps))
	for _, bp := range androidBps {
		if !toDrop[bp] {
			filtered = append(filtered, bp)
		}
	}
	return filtered
}

// existing{{.Camel}}StockParallel maps a lane bp to its stock parallel: the tree-prefix strip and, when
// that path doesn't exist, the IDENTITY-APP de-prefix (packages{{.DirSuffix}}/apps/{{.DirPrefix}}Foo/…
// → packages/apps/Foo/…). Returns "" for an additive-only fork ({{.Lane}}ForkKeepsStock) or when no
// stock parallel exists. The de-prefix branch is VAR-GUARDED: a stock parallel that declares a top-level
// blueprint variable is KEPT (un-forked stock subdirs inherit it — dropping it would orphan them).
func existing{{.Camel}}StockParallel(bp string) string {
	if {{.Lane}}ForkKeepsStock(bp) {
		return ""
	}
	var rel string
	if strings.HasPrefix(bp, "frameworks{{.DirSuffix}}/") {
		rel = "frameworks/" + strings.TrimPrefix(bp, "frameworks{{.DirSuffix}}/")
	} else if strings.HasPrefix(bp, "packages{{.DirSuffix}}/") {
		rel = "packages/" + strings.TrimPrefix(bp, "packages{{.DirSuffix}}/")
	} else {
		return ""
	}
	if _, err := os.Stat(rel); err == nil {
		return rel
	}
	if deprefixed := deprefix{{.Camel}}IdentitySegment(rel); deprefixed != rel {
		if _, err := os.Stat(deprefixed); err == nil {
			if {{.Lane}}BpDefinesTopLevelVariable(deprefixed) {
				return ""
			}
			return deprefixed
		}
	}
	return ""
}

// deprefix{{.Camel}}IdentitySegment reverses the identity-app rename on the FIRST path segment carrying
// the "{{.DirPrefix}}" identity prefix ({{.DirPrefix}}Foo → Foo). Returns the input unchanged when no
// such segment exists (or the prefix is empty — a rename lane without a dir prefix has no de-prefix).
func deprefix{{.Camel}}IdentitySegment(path string) string {
	const prefix = "{{.DirPrefix}}"
	if prefix == "" {
		return path
	}
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, prefix) && len(s) > len(prefix) {
			segs[i] = strings.TrimPrefix(s, prefix)
			return strings.Join(segs, "/")
		}
	}
	return path
}

// {{.Lane}}ForkKeepsStock reports whether a lane bp is ADDITIVE-only — every declared module name starts
// with the CamelCase shared-infra prefix "{{.Camel}}" — in which case its stock parallel is KEPT. A bp
// with any keep-name (non-"{{.Camel}}") module — including an identity-app {{.DirPrefix}}<App> bp whose
// internal test/license modules are keep-name — is a REPLACEMENT, so its stock parallel is dropped.
// Unreadable → replacement (drop).
func {{.Lane}}ForkKeepsStock(bp string) bool {
	contents, err := os.ReadFile(bp)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(contents), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "name:") {
			continue
		}
		q := strings.Index(t, "\"")
		if q < 0 {
			continue
		}
		rest := t[q+1:]
		e := strings.Index(rest, "\"")
		if e < 0 {
			continue
		}
		if name := rest[:e]; !strings.HasPrefix(name, "{{.Camel}}") {
			return false
		}
	}
	return true
}

// is{{.Camel}}OwnedNamespaceBp reports whether a lane namespace-decl bp must stay NAMESPACED (not
// collapse to root). ONLY decomposition pods (frameworks{{.DirSuffix}}/base/packages/*/pods/**) qualify:
// their per-layer modules carry short generic names (api/impl repeated across domain/data layers) that
// collide with EACH OTHER at root, so each pod dir keeps its own nested soong_namespace. Everything else
// — packages{{.DirSuffix}}/ apps, base/packages/ shared libs + identity apps + apex forks — COLLAPSES to
// root (Holo-style): unique-name or keep-name-twin-whose-stock-parallel-drops modules claim the canonical
// slot, so forked keep-name exports/apexes resolve for root/stock/generic consumers with NO re-export stub.
func is{{.Camel}}OwnedNamespaceBp(bp string) bool {
	return strings.Contains(bp, "/pods/")
}

// {{.Lane}}BpDefinesTopLevelVariable reports whether an Android.bp declares any top-level blueprint
// VARIABLE assignment (an unindented ident = … / ident += …), as opposed to only module definitions
// (whose properties are indented). Blueprint scopes such variables to the file AND its subdir bps, so
// dropping a definer orphans any un-dropped subdir bp that references it.
func {{.Lane}}BpDefinesTopLevelVariable(path string) bool {
	contents, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(contents), "\n") {
		if len(line) == 0 || line[0] == ' ' || line[0] == '\t' || line[0] == '/' {
			continue
		}
		eq := strings.Index(line, "=")
		if eq <= 0 {
			continue
		}
		lhs := strings.TrimSpace(strings.TrimSuffix(line[:eq], "+"))
		if lhs == "" {
			continue
		}
		allIdent := true
		for _, r := range lhs {
			if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
				allIdent = false
				break
			}
		}
		if allIdent {
			return true
		}
	}
	return false
}
`))

// finderKeepNameApplyTmpl — the KEEP-NAME model's apply (Holo, the proven Lane Sovereignty method).
// For every lane bp it KEEPS the lane bp and DROPS its stock parallel (per-file replacement — the
// lane bp replaces stock; keep-name so there is no collision). Lane root Android.bp + manifest
// namespace decls collapse to the global namespace (their stock parallel stays for shared consumers).
var finderKeepNameApplyTmpl = template.Must(template.New("keepnameapply").Parse(
	"// apply{{.Camel}}BpRoutes is the {{.Lane}} lane's BP filter (KEEP-NAME model — the proven Lane\n" +
		"// Sovereignty method). For every frameworks{{.DirSuffix}}/ | packages{{.DirSuffix}}/ bp it KEEPS the\n" +
		"// lane bp and DROPS its stock parallel (per-file replacement — the lane bp replaces stock; keep-name\n" +
		"// so no collision, mirroring the stock single-tree finder). Lane root Android.bp + manifest\n" +
		"// dropped_namespace_decl_paths collapse to the global namespace (their stock parallel STAYS for\n" +
		"// shared consumers). Other-lane bp are dropped (isOtherLaneBpFor{{.Camel}}).\n" +
		"func apply{{.Camel}}BpRoutes(ctx Context, config Config, androidBps []string) []string {\n" +
		"\tmanifest := load{{.Camel}}BpRouteManifest(ctx, config)\n" +
		"\ttoDrop := map[string]bool{}\n" +
		"\tif manifest != nil {\n" +
		"\t\tfor _, decl := range manifest.DroppedNamespaceDeclPaths {\n" +
		"\t\t\ttoDrop[decl] = true\n" +
		"\t\t}\n" +
		"\t}\n" +
		"\t// Lane roots declare a namespace → drop them so lane modules collapse to global (matching stock).\n" +
		"\ttoDrop[\"frameworks{{.DirSuffix}}/Android.bp\"] = true\n" +
		"\ttoDrop[\"packages{{.DirSuffix}}/Android.bp\"] = true\n" +
		"\tfor _, bp := range androidBps {\n" +
		"\t\tif isOtherLaneBpFor{{.Camel}}(bp) {\n" +
		"\t\t\ttoDrop[bp] = true\n" +
		"\t\t\tcontinue\n" +
		"\t\t}\n" +
		"\t\tif !strings.HasPrefix(bp, \"frameworks{{.DirSuffix}}/\") && !strings.HasPrefix(bp, \"packages{{.DirSuffix}}/\") {\n" +
		"\t\t\tcontinue\n" +
		"\t\t}\n" +
		"\t\tif toDrop[bp] {\n" +
		"\t\t\tcontinue // namespace-collapse drop (lane bp removed); its stock parallel is kept\n" +
		"\t\t}\n" +
		"\t\tif stock := {{.Lane}}StockParallel(bp); stock != \"\" {\n" +
		"\t\t\ttoDrop[stock] = true // per-file replacement: drop the stock parallel\n" +
		"\t\t}\n" +
		"\t}\n" +
		"\tfiltered := make([]string, 0, len(androidBps))\n" +
		"\tfor _, bp := range androidBps {\n" +
		"\t\tif !toDrop[bp] {\n" +
		"\t\t\tfiltered = append(filtered, bp)\n" +
		"\t\t}\n" +
		"\t}\n" +
		"\treturn filtered\n" +
		"}\n\n" +
		"// {{.Lane}}StockParallel maps a lane bp to its stock-parallel path (frameworks{{.DirSuffix}}/X →\n" +
		"// frameworks/X, packages{{.DirSuffix}}/X → packages/X), or \"\" when bp is not a lane bp.\n" +
		"func {{.Lane}}StockParallel(bp string) string {\n" +
		"\tif strings.HasPrefix(bp, \"frameworks{{.DirSuffix}}/\") {\n" +
		"\t\treturn \"frameworks/\" + strings.TrimPrefix(bp, \"frameworks{{.DirSuffix}}/\")\n" +
		"\t}\n" +
		"\tif strings.HasPrefix(bp, \"packages{{.DirSuffix}}/\") {\n" +
		"\t\treturn \"packages/\" + strings.TrimPrefix(bp, \"packages{{.DirSuffix}}/\")\n" +
		"\t}\n" +
		"\treturn \"\"\n" +
		"}\n"))

// gofmtSnippet formats a set of top-level Go declarations by wrapping them in a throwaway
// package, running go/format, and stripping the package line — so a spliced block is gofmt-clean
// without reflowing the rest of the target file.
func gofmtSnippet(decls string) (string, error) {
	wrapped := "package p\n\n" + decls
	b, err := format.Source([]byte(wrapped))
	if err != nil {
		return "", fmt.Errorf("gofmt snippet: %w", err)
	}
	out := strings.TrimPrefix(string(b), "package p\n\n")
	return out, nil
}

// genFinderLaneFuncs renders + gofmt-normalizes the 5 additive finder funcs for the lane.
// otherSuffixes is the set of OTHER lanes' dir suffixes (e.g. ["-holo","-nexus"]) this lane
// must be blind to — derived from the tree's current isLaneLunch minus the new lane.
func genFinderLaneFuncs(c LaneConfig, otherSuffixes []string) (string, error) {
	if len(otherSuffixes) == 0 {
		// A first lane on a fresh tree: nothing to drop. Emit a chain that never matches.
		otherSuffixes = nil
	}
	parts := make([]string, 0, len(otherSuffixes))
	for _, s := range otherSuffixes {
		parts = append(parts, fmt.Sprintf("strings.HasSuffix(comp, %q)", s))
	}
	chain := strings.Join(parts, " ||\n\t\t\t")
	if chain == "" {
		chain = "false" // no sibling lanes yet
	}
	data := finderTmplData{Lane: c.Name, Camel: c.CamelCase, DirSuffix: c.DirSuffix, DirPrefix: c.DirPrefix, SuffixChain: chain}
	var sb strings.Builder
	if err := finderSharedTmpl.Execute(&sb, data); err != nil {
		return "", err
	}
	sb.WriteString("\n")
	// KEEP-NAME (Holo) → per-file replacement; RENAME (NexusM) → drop-other + manifest only.
	applyTmpl := finderRenameApplyTmpl
	if c.KeepName {
		applyTmpl = finderKeepNameApplyTmpl
	}
	if err := applyTmpl.Execute(&sb, data); err != nil {
		return "", err
	}
	return gofmtSnippet(sb.String())
}

// extractStringSlice returns the elements of the first []string{...} literal in the named func.
func extractStringSlice(src []byte, funcName string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return nil, err
	}
	var elems []string
	var found bool
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != funcName || fd.Body == nil {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if found {
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
			if id, ok := at.Elt.(*ast.Ident); !ok || id.Name != "string" {
				return true
			}
			for _, e := range cl.Elts {
				if bl, ok := e.(*ast.BasicLit); ok && bl.Kind == token.STRING {
					if v, uerr := strconv.Unquote(bl.Value); uerr == nil {
						elems = append(elems, v)
					}
				}
			}
			found = true
			return false
		})
		break
	}
	if !found {
		return nil, fmt.Errorf("no []string{...} in func %q", funcName)
	}
	return elems, nil
}

// deriveOtherLaneSuffixes reads the tree's isLaneLunch suffixes (aar.go source) and maps each
// "_lane" → "-lane", excluding the new lane. This is the set the new lane must drop.
func deriveOtherLaneSuffixes(aarSrc []byte, newLane string) ([]string, error) {
	lunchSuffixes, err := extractStringSlice(aarSrc, "isLaneLunch")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range lunchSuffixes {
		lane := strings.TrimPrefix(s, "_")
		if lane == newLane {
			continue
		}
		out = append(out, "-"+lane)
	}
	return out, nil
}

// insertBeforeFunc splices block (a gofmt'd set of decls) immediately before the named func's
// declaration (or its doc comment), separated by a blank line. Idempotent: if a func named
// is<Camel>Lane already exists, returns src unchanged. Re-parses to prove the result compiles.
func insertBeforeFunc(src []byte, beforeFunc, guardFunc, block string) (out []byte, changed bool, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, false, fmt.Errorf("parse: %w", err)
	}
	var target *ast.FuncDecl
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fd.Name.Name == guardFunc {
			return src, false, nil // already inserted — idempotent
		}
		if fd.Name.Name == beforeFunc && target == nil {
			target = fd
		}
	}
	if target == nil {
		return nil, false, fmt.Errorf("anchor func %q not found", beforeFunc)
	}
	pos := target.Pos()
	if target.Doc != nil {
		pos = target.Doc.Pos()
	}
	off := lineStartOffset(fset, pos)
	ins := []byte(strings.TrimRight(block, "\n") + "\n\n")
	out = append(append(append([]byte{}, src[:off]...), ins...), src[off:]...)
	if _, perr := parser.ParseFile(token.NewFileSet(), "", out, 0); perr != nil {
		return nil, false, fmt.Errorf("post-insert reparse failed: %w", perr)
	}
	return out, true, nil
}

// appendDeclsAtEnd appends block (gofmt'd decls) at end-of-file. Idempotent: no-op if guardFunc
// already exists. Fallback for a pristine tree with no sibling lane func to anchor before.
func appendDeclsAtEnd(src []byte, guardFunc, block string) (out []byte, changed bool, err error) {
	f, perr := parser.ParseFile(token.NewFileSet(), "", src, parser.ParseComments)
	if perr != nil {
		return nil, false, fmt.Errorf("parse: %w", perr)
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == guardFunc {
			return src, false, nil
		}
	}
	s := string(src)
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	s += "\n" + strings.TrimRight(block, "\n") + "\n"
	out = []byte(s)
	if _, perr := parser.ParseFile(token.NewFileSet(), "", out, 0); perr != nil {
		return nil, false, fmt.Errorf("post-append reparse failed: %w", perr)
	}
	return out, true, nil
}

// insertLaneFuncs inserts the additive block before a known sibling lane func if one exists,
// else appends at end-of-file — robust for both a seeded tree (this holo tree) and a pristine
// AOSP tree. Idempotent via the is<Camel>Lane guard.
func insertLaneFuncs(src []byte, camel, block string) ([]byte, bool, error) {
	guard := "is" + camel + "Lane"
	for _, anchor := range []string{"isNexusMNexus", "isHoloProduct", "isProductProduct"} {
		out, changed, err := insertBeforeFunc(src, anchor, guard, block)
		if err == nil {
			return out, changed, nil // anchor found (or idempotent no-op)
		}
	}
	return appendDeclsAtEnd(src, guard, block)
}

// insertPipelineCall splices `if is<Camel>Lane(config) { androidBps = apply<Camel>BpRoutes(...) }`
// immediately before the `if len(androidBps) == 0` guard in the finder pipeline. Idempotent.
func insertPipelineCall(src []byte, camel string) (out []byte, changed bool, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, false, fmt.Errorf("parse: %w", err)
	}
	callMarker := "is" + camel + "Lane(config)"
	if strings.Contains(string(src), callMarker) {
		return src, false, nil // already wired
	}
	var target *ast.IfStmt
	ast.Inspect(f, func(n ast.Node) bool {
		if target != nil {
			return false
		}
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		be, ok := ifs.Cond.(*ast.BinaryExpr)
		if !ok || be.Op != token.EQL {
			return true
		}
		ce, ok := be.X.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := ce.Fun.(*ast.Ident)
		if !ok || id.Name != "len" || len(ce.Args) != 1 {
			return true
		}
		if arg, ok := ce.Args[0].(*ast.Ident); ok && arg.Name == "androidBps" {
			target = ifs
			return false
		}
		return true
	})
	if target == nil {
		return nil, false, fmt.Errorf("pipeline guard `if len(androidBps) == 0` not found")
	}
	off := lineStartOffset(fset, target.Pos())
	block := fmt.Sprintf("\tif is%sLane(config) {\n\t\tandroidBps = apply%sBpRoutes(ctx, config, androidBps)\n\t}\n", camel, camel)
	ins := []byte(block)
	out = append(append(append([]byte{}, src[:off]...), ins...), src[off:]...)
	if _, perr := parser.ParseFile(token.NewFileSet(), "", out, 0); perr != nil {
		return nil, false, fmt.Errorf("post-insert reparse failed: %w", perr)
	}
	return out, true, nil
}

// lineStartOffset returns the byte offset of the first column on the line containing pos.
// Indentation in soong is tabs (1 byte / 1 column), so Column-1 bytes precede the token.
func lineStartOffset(fset *token.FileSet, pos token.Pos) int {
	p := fset.Position(pos)
	return p.Offset - (p.Column - 1)
}
