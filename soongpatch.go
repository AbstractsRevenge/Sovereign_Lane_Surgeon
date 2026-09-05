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
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	// KeptStockBpPaths: stock bps kept although a lane parallel exists (additive lane dirs), written by
	// UX Design Governance's `govern route-curate`; the generated finder honors them.
	KeptStockBpPaths []string `json:"kept_stock_bp_paths"`
	// DerivedFrom names the lane this one was SEEDED FROM (create -from), empty for a
	// stock-seeded lane. It makes the tree self-describing: a downstream tool that has
	// no authored config for this lane can ask what it came from and inherit that
	// lane's, instead of every tool carrying its own switch of known lane names.
	DerivedFrom string `json:"derived_from,omitempty"`
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
	return emitRouteManifestFrom(lane, camel, outRoot, "")
}

// inheritDropsFromLane returns srcLane's curated dropped_namespace_decl_paths, remapped onto the
// new lane. A lane-sourced fork is a VERBATIM clone, so it displaces exactly the stock parallels
// the source lane displaces — and it inherits every directory RELOCATION the source made.
//
// That inheritance is why the seed list is not enough. The finder computes a lane bp's stock
// parallel from its path, which silently fails whenever the source lane moved a directory
// (frameworks-<src>/…/vts/kotlin has no frameworks/…/vts/kotlin parallel, so stock's vts/java
// stays loaded and both define the same modules) or the stock parallel lives under a different
// root (packages-<src>/system/… vs system/…). Those drops cannot be derived from the new lane's
// tree; they only exist as the source lane's accumulated curation.
func inheritDropsFromLane(srcLane, newLane, outRoot string) ([]string, error) {
	p := filepath.Join(outRoot, "."+srcLane, srcLane+"_bp_route_manifest.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read source lane manifest %s: %w", p, err)
	}
	var src struct {
		DroppedNamespaceDeclPaths []string `json:"dropped_namespace_decl_paths"`
	}
	if err := json.Unmarshal(b, &src); err != nil {
		return nil, fmt.Errorf("parse source lane manifest %s: %w", p, err)
	}
	oldFw, newFw := "frameworks-"+srcLane+"/", "frameworks-"+newLane+"/"
	oldPkg, newPkg := "packages-"+srcLane+"/", "packages-"+newLane+"/"
	out := make([]string, 0, len(src.DroppedNamespaceDeclPaths))
	for _, d := range src.DroppedNamespaceDeclPaths {
		switch {
		case strings.HasPrefix(d, oldFw):
			d = newFw + strings.TrimPrefix(d, oldFw)
		case strings.HasPrefix(d, oldPkg):
			d = newPkg + strings.TrimPrefix(d, oldPkg)
		}
		out = append(out, d)
	}
	return out, nil
}

// emitRouteManifestFrom renders the seed manifest, optionally inheriting srcLane's curation.
func emitRouteManifestFrom(lane, camel, outRoot, srcLane string) ([]byte, error) {
	drops := []string{}
	for _, p := range knownStockDangles {
		if outRoot != "" {
			if _, err := os.Stat(filepath.Join(outRoot, p)); err != nil {
				continue // not present in this checkout — skip
			}
		}
		drops = append(drops, p)
	}
	if srcLane != "" && outRoot != "" {
		inherited, err := inheritDropsFromLane(srcLane, lane, outRoot)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, d := range append(drops, inherited...) {
			seen[d] = true
		}
		drops = drops[:0]
		for d := range seen {
			drops = append(drops, d)
		}
		sort.Strings(drops)
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
		KeptStockBpPaths:          []string{},
		DerivedFrom:               srcLane,
	}
	return json.MarshalIndent(m, "", "  ")
}

// ─────────────────────────────────────────────────────────────────────────────────────────────
// LANE ALLOWLIST DISCOVERY — shared-tree .go files that carry per-lane path entries.
//
// build/soong is NOT the only place lane paths must be registered. ANY Soong plugin anywhere in
// the tree may call android.AddNeverAllowRules or keep its own lane-path list, and the surgeon's
// fixed patch set (aar/visibility/finder/neverallow) cannot reach them. Found the hard way on
// holo2test (2026-08-20): external/icu/build/icu.go carries libandroidicu/libicuuc allowlists whose
// entries include "packages-holo/modules/RuntimeI18n/apex/". A lane missing from that list fails
// with "violates neverallow requirements" — an error naming neither the lane nor the file.
//
// A FIXED list of known sites would be a false promise: the next plugin to add one would not be on
// it. So this DISCOVERS them instead, from a property that is true by construction:
//
//	    a .go file already containing a "<root>-<some-existing-lane>/…" string literal
//	    IS a lane allowlist, and this lane belongs beside that entry.
//
// Self-maintaining: a plugin added upstream tomorrow is found the moment any lane registers in it.
// Verified against the live tree — discovery independently re-finds all four sites that were
// located by hand: external/icu/build/icu.go, build/soong/aconfig/all_aconfig_declarations.go,
// build/soong/android/androidmk.go, build/soong/android/apex.go.
// ─────────────────────────────────────────────────────────────────────────────────────────────

// discoverNeverAllowSites returns every .go file OUTSIDE build/soong that calls
// android.AddNeverAllowRules and names a stock frameworks/ or packages/ path. Such a plugin
// restricts a dependency to an allowlist of directories, and a lane that forks one of those
// directories fails with "violates neverallow requirements" the first time the lane builds.
//
// discoverLaneAllowlists cannot find these on a FIRST lane: it keys on an existing
// "<root>-<lane>/" literal, and a pristine tree has none. This keys on the CALL instead, which is
// true by construction of what the file is. Found the hard way on android-17 (2026-09-05):
// external/icu/build/icu.go allowlists packages/modules/RuntimeI18n/ for libandroidicu, and the
// full packages/ fork carried packages-holo/modules/RuntimeI18n/apex — refused at Soong analysis.
// The fix is the same rule PatchNeverallowLanePaths applies to build/soong/android/neverallow.go:
// every stock path gets its lane twin beside it. Skip rules match discoverLaneAllowlists.
func discoverNeverAllowSites(outRoot string) []string {
	var hits []string
	filepath.Walk(outRoot, func(p string, fi os.FileInfo, e error) error {
		if e != nil {
			return nil
		}
		base := filepath.Base(p)
		if fi.IsDir() {
			switch {
			case base == ".git", base == ".repo", strings.HasPrefix(base, ".sld-"),
				strings.HasPrefix(base, "out") && filepath.Dir(p) == outRoot,
				strings.HasPrefix(base, "frameworks-"), strings.HasPrefix(base, "packages-"),
				base == "_snapshots", strings.Contains(base, "snapshot"),
				p == filepath.Join(outRoot, "build", "soong"): // owned by the dedicated patchers
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) != ".go" || strings.HasSuffix(p, "_test.go") || fi.Size() > 4<<20 {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		src := string(b)
		if !strings.Contains(src, "AddNeverAllowRules(") {
			return nil
		}
		if !strings.Contains(src, `"frameworks/`) && !strings.Contains(src, `"packages/`) {
			return nil
		}
		if rel, rerr := filepath.Rel(outRoot, p); rerr == nil {
			hits = append(hits, rel)
		}
		return nil
	})
	sort.Strings(hits)
	return hits
}

// knownLaneSuffixes returns the lane names already present in the tree (from frameworks-*/ dirs),
// excluding the lane being created. These are the tokens that mark a list as lane-aware.
func knownLaneSuffixes(outRoot, newLane string) []string {
	ents, err := os.ReadDir(outRoot)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "frameworks-") {
			continue
		}
		if lane := strings.TrimPrefix(e.Name(), "frameworks-"); lane != "" && lane != newLane {
			out = append(out, lane)
		}
	}
	sort.Strings(out)
	return out
}

// laneAllowlistHit is one discovered insertion: a file, and the literals to add.
type laneAllowlistHit struct {
	rel   string
	adds  []string // the new lane's parallels, in first-seen order
	model string   // the existing entry each was derived from (for the report)
}

// discoverLaneAllowlists walks the tree for .go files holding lane-path string literals and
// computes this lane's parallel for each. Skips out*/.git/.repo, every lane tree (a lane's own
// sources are not shared infrastructure), and .sld-* staging/snapshot dirs.
func discoverLaneAllowlists(outRoot, newLane string) []laneAllowlistHit {
	lanes := knownLaneSuffixes(outRoot, newLane)
	if len(lanes) == 0 {
		return nil
	}
	// Files the DEDICATED patchers already own. finder.go and neverallow.go are full of lane
	// literals by design — discovery would "find" them and fight the purpose-built patch that
	// understands their structure. Ownership beats pattern-matching.
	owned := map[string]bool{
		filepath.Join("build", "soong", "ui", "build", "finder.go"): true,
		filepath.Join("build", "soong", "android", "neverallow.go"): true,
		filepath.Join("build", "soong", "android", "visibility.go"): true,
		filepath.Join("build", "soong", "java", "aar.go"):           true,
	}
	var hits []laneAllowlistHit
	filepath.Walk(outRoot, func(p string, fi os.FileInfo, e error) error {
		if e != nil {
			return nil
		}
		base := filepath.Base(p)
		if fi.IsDir() {
			switch {
			case base == ".git", base == ".repo", strings.HasPrefix(base, ".sld-"),
				strings.HasPrefix(base, "out") && filepath.Dir(p) == outRoot,
				strings.HasPrefix(base, "frameworks-"), strings.HasPrefix(base, "packages-"),
				// Archived copies of patched soong sources are not live infrastructure. Patching
				// one is harmless but reports a site that does not exist, which is worse than
				// silence — it teaches a false map of the tree.
				base == "_snapshots", strings.Contains(base, "snapshot"):
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) != ".go" || fi.Size() > 4<<20 {
			return nil
		}
		if rel, rerr := filepath.Rel(outRoot, p); rerr == nil && owned[rel] {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		src := string(b)
		seen := map[string]bool{}
		var adds []string
		model := ""
		for _, lane := range lanes {
			for _, root := range []string{"frameworks", "packages"} {
				tok := root + "-" + lane + "/"
				idx := 0
				for {
					i := strings.Index(src[idx:], `"`+tok)
					if i < 0 {
						break
					}
					i += idx
					j := strings.IndexByte(src[i+1:], '"')
					if j < 0 {
						break
					}
					lit := src[i+1 : i+1+j]
					idx = i + 1 + j
					mine := root + "-" + newLane + "/" + strings.TrimPrefix(lit, tok)
					if seen[mine] || strings.Contains(src, `"`+mine+`"`) {
						continue // idempotent: already registered
					}
					seen[mine] = true
					adds = append(adds, mine)
					if model == "" {
						model = lit
					}
				}
			}
		}
		if len(adds) > 0 {
			rel, _ := filepath.Rel(outRoot, p)
			hits = append(hits, laneAllowlistHit{rel: rel, adds: adds, model: model})
		}
		return nil
	})
	sort.Slice(hits, func(i, j int) bool { return hits[i].rel < hits[j].rel })
	return hits
}

// patchLaneAllowlistFile inserts each new literal immediately after the existing entry it was
// derived from, preserving that line's exact indentation. Parsed with go/ast first and re-parsed
// after, so a patch that would not compile is refused rather than written (HARD RULE 3 — the AST
// decides, never a regex over structure).
func patchLaneAllowlistFile(src []byte, hit laneAllowlistHit, newLane string) ([]byte, bool, error) {
	if _, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ParseComments); err != nil {
		return nil, false, fmt.Errorf("pre-parse: %w", err)
	}
	lines := strings.Split(string(src), "\n")
	changed := false
	for _, add := range hit.adds {
		// derive the model literal for THIS add (same tail, an existing lane's root)
		tail := add
		if k := strings.Index(add, "/"); k >= 0 {
			tail = add[k:]
		}
		insertAt, indent := -1, ""
		for i, ln := range lines {
			if !strings.Contains(ln, `"`) || !strings.Contains(ln, tail) {
				continue
			}
			if strings.Contains(ln, `"`+add+`"`) { // already present
				insertAt = -1
				break
			}
			if regexpLaneLiteral.MatchString(ln) {
				insertAt = i
				indent = ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
			}
		}
		if insertAt < 0 {
			continue
		}
		entry := indent + `"` + add + `",`
		lines = append(lines[:insertAt+1], append([]string{entry}, lines[insertAt+1:]...)...)
		changed = true
	}
	if !changed {
		return src, false, nil
	}
	out := []byte(strings.Join(lines, "\n"))
	if _, err := parser.ParseFile(token.NewFileSet(), "", out, parser.ParseComments); err != nil {
		return nil, false, fmt.Errorf("post-parse (patch refused): %w", err)
	}
	return out, true, nil
}

// regexpLaneLiteral matches a line holding a lane-root string literal — the shape that marks a
// line as an allowlist entry rather than prose.
var regexpLaneLiteral = regexp.MustCompile(`"(frameworks|packages)-[a-z0-9]+/`)
