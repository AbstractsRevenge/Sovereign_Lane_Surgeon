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
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Hermetic samples mirroring the real slices (aar.go:isLaneLunch, visibility.go:laneCanonicalPkgs).
const isLaneLunchSample = `package build

func isLaneLunch(productName string) bool {
	for _, suffix := range []string{"_holo", "_nexus", "_nexusm"} {
		if strings.HasSuffix(productName, suffix) {
			return true
		}
	}
	return false
}
`

const laneCanonicalPkgsSample = `package android

func laneCanonicalPkgs(pkg string) []string {
	for _, suf := range []string{"-holo", "-nexusm", "-nexus"} {
		_ = suf
	}
	return nil
}
`

// TestAppendStringElemReproduce: strip a lane from the real-shaped sample, re-add via the
// patcher, and require byte-identical reproduction — proof the codegen reproduces the
// hand-written registration exactly (isLaneLunch, where nexusm is the LAST element).
func TestAppendStringElemReproduce(t *testing.T) {
	stripped := strings.Replace(isLaneLunchSample, `, "_nexusm"`, "", 1)
	if stripped == isLaneLunchSample {
		t.Fatal("strip no-op — sample changed?")
	}
	out, changed, err := PatchIsLaneLunch([]byte(stripped), "nexusm")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if string(out) != isLaneLunchSample {
		t.Errorf("not byte-identical to original:\n--- got ---\n%s\n--- want ---\n%s", out, isLaneLunchSample)
	}
}

// TestAppendStringElemSetEqual: laneCanonicalPkgs has nexusm in the MIDDLE, so append-to-end
// reproduces the same SET (semantically identical for a suffix-match loop), order differs.
func TestAppendStringElemSetEqual(t *testing.T) {
	stripped := strings.Replace(laneCanonicalPkgsSample, `"-nexusm", `, "", 1)
	out, changed, err := PatchLaneCanonicalPkgs([]byte(stripped), "nexusm")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	for _, want := range []string{`"-holo"`, `"-nexus"`, `"-nexusm"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing suffix %s in patched output", want)
		}
	}
}

// TestAppendStringElemIdempotent: patching an already-registered lane is a no-op.
func TestAppendStringElemIdempotent(t *testing.T) {
	out, changed, err := PatchIsLaneLunch([]byte(isLaneLunchSample), "nexusm")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("expected changed=false for already-present lane")
	}
	if string(out) != isLaneLunchSample {
		t.Error("idempotent patch mutated source")
	}
}

// TestAppendStringElemFreshLane: a brand-new lane appends cleanly and the result still parses.
func TestAppendStringElemFreshLane(t *testing.T) {
	out, changed, err := PatchIsLaneLunch([]byte(isLaneLunchSample), "aurora")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if !strings.Contains(string(out), `"_holo", "_nexus", "_nexusm", "_aurora"`) {
		t.Errorf("fresh lane not appended in order: %s", out)
	}
}

// TestAppendStringElemMissingFunc errors cleanly when the target func is absent.
func TestAppendStringElemMissingFunc(t *testing.T) {
	_, _, err := appendStringElem([]byte(isLaneLunchSample), "noSuchFunc", "_x")
	if err == nil {
		t.Error("expected error for missing func")
	}
}

// TestEmitRouteManifest: valid JSON; seeds known stock danglers so the lane is zero-flag; the
// per-checkout existence filter drops absent ones.
func TestEmitRouteManifest(t *testing.T) {
	// outRoot="" (dry-run): assume-present → all knownStockDangles seeded (zero-flag default).
	b, err := emitRouteManifest("aurora", "Aurora", "")
	if err != nil {
		t.Fatal(err)
	}
	var m routeManifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("emitted invalid JSON: %v", err)
	}
	if m.Lane != "aurora" || m.SchemaVersion != "1.0" {
		t.Errorf("bad header: lane=%q ver=%q", m.Lane, m.SchemaVersion)
	}
	if len(m.DroppedNamespaceDeclPaths) != len(knownStockDangles) {
		t.Errorf("dry-run should seed all %d known danglers, got %d", len(knownStockDangles), len(m.DroppedNamespaceDeclPaths))
	}
	found := false
	for _, p := range m.DroppedNamespaceDeclPaths {
		if p == "bootable/deprecated-ota/Android.bp" {
			found = true
		}
	}
	if !found {
		t.Error("must seed bootable/deprecated-ota drop (the updater dangler)")
	}
	if !strings.Contains(m.Description, "ALLOW_MISSING_DEPENDENCIES") {
		t.Error("description should explain the zero-flag rationale")
	}
	// Real (empty) outRoot: none of the danglers exist there → filtered to 0.
	b2, _ := emitRouteManifest("aurora", "Aurora", t.TempDir())
	var m2 routeManifest
	json.Unmarshal(b2, &m2)
	if len(m2.DroppedNamespaceDeclPaths) != 0 {
		t.Errorf("empty tree should filter all danglers, got %d", len(m2.DroppedNamespaceDeclPaths))
	}
}

// TestAppendFrameworkResCase: adds framework-res-<lane> to the switch; gofmt-clean; idempotent.
func TestAppendFrameworkResCase(t *testing.T) {
	src := `package java

func isFrameworkResClassName(name string) bool {
	switch name {
	case "framework-res",
		"framework-res-nexus",
		"framework-res-nexusm":
		return true
	}
	return false
}
`
	out, changed, err := appendFrameworkResCase([]byte(src), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	if !strings.Contains(string(out), `"framework-res-t1"`) {
		t.Errorf("case not added:\n%s", out)
	}
	if f, ferr := format.Source(out); ferr != nil || string(f) != string(out) {
		t.Errorf("not gofmt-clean:\n%s", out)
	}
	out2, changed2, _ := appendFrameworkResCase(out, "t1")
	if changed2 || string(out2) != string(out) {
		t.Error("second append should be a no-op")
	}
}

// --- v0.3.0: lane-name collision guard + drop-list inheritance ---

// The name IS a finder path predicate, so a name any existing directory already ends with makes
// every lane blind to those directories.
func TestCheckLaneSuffixCollision(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{
		"prebuilts/misc/common/androidx-test",
		"external/kotlinx.coroutines/kotlinx-coroutines-test",
		"frameworks-holotest/base", // the lane's OWN root must never count against it
		"packages-holotest/apps",
		"external/guava",
	} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	off, total := checkLaneSuffixCollision(root, "test")
	if total != 2 {
		t.Errorf("want 2 colliding dirs for %q, got %d (%v)", "test", total, off)
	}
	if _, total := checkLaneSuffixCollision(root, "holotest"); total != 0 {
		t.Errorf("lane's own roots must not count as collisions, got %d", total)
	}
}

// A lane-sourced fork inherits the source lane's directory RELOCATIONS, which cannot be derived
// from the new lane's tree. Lane-own entries are remapped; stock entries carry over verbatim.
func TestInheritDropsFromLane(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".holo"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := `{"dropped_namespace_decl_paths":[
	  "frameworks/hardware/interfaces/stats/aidl/vts/java/Android.bp",
	  "packages-holo/system/secretkeeper/client/Android.bp",
	  "frameworks-holo/ex/camera2/extensions/Android.bp",
	  "bootable/deprecated-ota/Android.bp"
	]}`
	if err := os.WriteFile(filepath.Join(root, ".holo", "holo_bp_route_manifest.json"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := inheritDropsFromLane("holo", "holotest", root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"frameworks/hardware/interfaces/stats/aidl/vts/java/Android.bp": true, // stock: verbatim
		"packages-holotest/system/secretkeeper/client/Android.bp":       true, // lane-own: remapped
		"frameworks-holotest/ex/camera2/extensions/Android.bp":          true, // lane-own: remapped
		"bootable/deprecated-ota/Android.bp":                            true, // stock: verbatim
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %v", len(got), len(want), got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected entry %q", g)
		}
	}
}
