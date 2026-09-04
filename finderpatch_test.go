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
	"go/format"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

const finderStub = `package build

import "strings"

func isNexusMNexus(config Config) bool { return false }

func runFinder() {
	androidBps := find()
	androidBps = applyHoloBpRoutes(androidBps)
	if len(androidBps) == 0 {
		panic("none")
	}
	_ = androidBps
}
`

func mustParse(t *testing.T, src []byte) {
	t.Helper()
	if _, err := parser.ParseFile(token.NewFileSet(), "", src, 0); err != nil {
		t.Fatalf("result does not parse: %v\n%s", err, src)
	}
}

// TestGenFinderLaneFuncs: the 5 additive funcs render gofmt-clean with the nexusm self-contained
// shape (detection, manifest struct, loader, apply, isOtherLaneBpFor) + correct suffix chain.
func TestGenFinderLaneFuncs(t *testing.T) {
	cfg := deriveLane("aurora", true, nil, true, true, "")
	block, err := genFinderLaneFuncs(cfg, []string{"-holo", "-nexus"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"func isAuroraLane(config Config) bool",
		`strings.HasSuffix(targetProduct, "_aurora")`,
		"type auroraBpRouteManifest struct",
		"func loadAuroraBpRouteManifest(ctx Context, config Config) *auroraBpRouteManifest",
		`filepath.Join(".aurora", "aurora_bp_route_manifest.json")`,
		"func applyAuroraBpRoutes(ctx Context, config Config, androidBps []string) []string",
		"isOtherLaneBpForAurora(bp)",
		"func isOtherLaneBpForAurora(bp string) bool",
		`strings.HasPrefix(bp, "external/kotlinc-holo/")`,
		`strings.HasSuffix(comp, "-holo")`,
		`strings.HasSuffix(comp, "-nexus")`,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("generated block missing: %q", want)
		}
	}
	// gofmt-clean: wrapping + reparse (the block is decls only).
	mustParse(t, []byte("package p\n\n"+block))
}

// TestGenFinderModelAware: keep-name generates per-file replacement (StockParallel + stock-drop);
// rename generates the drop-other-only apply (no StockParallel func).
func TestGenFinderModelAware(t *testing.T) {
	keep := deriveLane("aurora", true, nil, true, true, "") // KeepName=true → Holo model
	kblock, err := genFinderLaneFuncs(keep, []string{"-holo"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"func auroraStockParallel(bp string) string",
		`toDrop["frameworks-aurora/Android.bp"] = true`,
		"toDrop[stock] = true", // per-file replacement
		"KEEP-NAME model",
	} {
		if !strings.Contains(kblock, want) {
			t.Errorf("keep-name block missing %q", want)
		}
	}
	mustParse(t, []byte("package p\n\n"+kblock))

	rename := deriveLane("aurora", false, nil, true, true, "AuroraM") // KeepName=false → RENAME/Model-A
	rblock, err := genFinderLaneFuncs(rename, []string{"-holo"})
	if err != nil {
		t.Fatal(err)
	}
	// Model-A rename now does PER-FILE stock-parallel replacement WITH the identity-app de-prefix
	// (formerly the rename model was finder-inert: drop-other-lane + manifest only).
	for _, want := range []string{
		"RENAME / Model-A hybrid",                                // marker
		"func existingAuroraStockParallel(bp string) string",     // de-prefixing stock-parallel helper
		"func deprefixAuroraIdentitySegment(path string) string", // identity-segment de-prefix
		`const prefix = "AuroraM"`,                               // DirPrefix threaded through
		"func auroraForkKeepsStock(bp string) bool",              // additive short-circuit
		"func auroraBpDefinesTopLevelVariable(path string) bool", // var-guard
		"toDrop[mapped] = true",                                  // per-file replacement
	} {
		if !strings.Contains(rblock, want) {
			t.Errorf("rename block missing %q", want)
		}
	}
	mustParse(t, []byte("package p\n\n"+rblock))
}

// TestInsertBeforeFunc: splice the generated funcs before a sibling; result parses; idempotent.
func TestInsertBeforeFunc(t *testing.T) {
	cfg := deriveLane("aurora", true, nil, true, true, "")
	block, err := genFinderLaneFuncs(cfg, []string{"-holo", "-nexus"})
	if err != nil {
		t.Fatal(err)
	}
	out, changed, err := insertBeforeFunc([]byte(finderStub), "isNexusMNexus", "isAuroraLane", block)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	mustParse(t, out)
	if !strings.Contains(string(out), "func isAuroraLane") {
		t.Error("inserted funcs missing")
	}
	// idempotent: re-run is a no-op (guard func now present).
	out2, changed2, err := insertBeforeFunc(out, "isNexusMNexus", "isAuroraLane", block)
	if err != nil {
		t.Fatal(err)
	}
	if changed2 || string(out2) != string(out) {
		t.Error("second insert should be a no-op")
	}
}

// TestInsertPipelineCall: splice the gated apply call before the len==0 guard; parses; idempotent.
func TestInsertPipelineCall(t *testing.T) {
	out, changed, err := insertPipelineCall([]byte(finderStub), "Aurora")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	mustParse(t, out)
	if !strings.Contains(string(out), "if isAuroraLane(config) {") ||
		!strings.Contains(string(out), "androidBps = applyAuroraBpRoutes(ctx, config, androidBps)") {
		t.Errorf("pipeline call not inserted correctly:\n%s", out)
	}
	// The new block must precede the guard.
	if strings.Index(string(out), "isAuroraLane") > strings.Index(string(out), "len(androidBps) == 0") {
		t.Error("inserted block should come before the len==0 guard")
	}
	// idempotent
	out2, changed2, _ := insertPipelineCall(out, "Aurora")
	if changed2 || string(out2) != string(out) {
		t.Error("second pipeline insert should be a no-op")
	}
}

// TestDeriveOtherLaneSuffixes: map isLaneLunch's _lane suffixes → -lane, excluding the new lane.
func TestDeriveOtherLaneSuffixes(t *testing.T) {
	got, err := deriveOtherLaneSuffixes([]byte(isLaneLunchSample), "aurora")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-holo", "-nexus", "-nexusm"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("aurora: got %v want %v", got, want)
	}
	// A lane already registered excludes itself.
	got2, _ := deriveOtherLaneSuffixes([]byte(isLaneLunchSample), "nexusm")
	if strings.Join(got2, ",") != "-holo,-nexus" {
		t.Errorf("nexusm: got %v want [-holo -nexus]", got2)
	}
}

// The keep-name apply must carry the namespace-collapse branch AND emit the predicate it calls.
// Without the branch a lane subtree keeps its own soong_namespace and stock consumers stop
// resolving its modules by bare name; without the predicate the generated finder does not compile.
func TestKeepNameApplyEmitsNamespaceCollapse(t *testing.T) {
	c := deriveLane("zed", true, nil, false, false, "")
	block, err := genFinderLaneFuncs(c, []string{"-holo"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"bpDeclaresNamespace(bp) && !isZedOwnedNamespaceBp(bp)",
		"func isZedOwnedNamespaceBp(bp string) bool",
		`strings.Contains(bp, "/pods/")`,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("keep-name block missing %q", want)
		}
	}
	// the emitted block must be valid Go (it is spliced into finder.go)
	if _, err := format.Source([]byte("package build\n\n" + block)); err != nil {
		t.Errorf("generated keep-name block does not compile: %v", err)
	}
}
