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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGeneratedHeaderProducer pins the mapping that made the stock-seeded fork tractable: a
// generated header NEVER exists in the source tree, so an existence guard must test the .proto
// that emits it. Two AOSP generators, two different suffixes — getting either wrong silently
// declines every rewrite it should have made (67 of them on holo2test).
func TestGeneratedHeaderProducer(t *testing.T) {
	cases := []struct{ in, want string }{
		{"base/core/proto/android/os/incident.pb.h", "base/core/proto/android/os/incident.proto"},                          // protobuf C++
		{"base/core/proto/android/service/sensor_service.proto.h", "base/core/proto/android/service/sensor_service.proto"}, // streaming_proto
		{"base/core/java/Foo.h", "base/core/java/Foo.h"},                                                                   // not generated — identity
		{"base/some/thing.proto", "base/some/thing.proto"},                                                                 // already a source — identity
	}
	for _, c := range cases {
		if got := generatedHeaderProducer(c.in); got != c.want {
			t.Errorf("generatedHeaderProducer(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRunRelocateStockSourcePaths_BothRootsAndProducer covers the three defects the holo2test
// build exposed in the first revision of this pass:
//  1. only `frameworks/` was matched, silently leaving `packages/` imports (17 across 13 files)
//  2. the guard tested the generated header, which never exists, so it declined valid rewrites
//  3. a reference with NO lane parallel must stay stock (the un-forked-subtree case)
func TestRunRelocateStockSourcePaths_BothRootsAndProducer(t *testing.T) {
	root := t.TempDir()
	lane := "aurora"
	mk := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Lane owns these producers…
	mk("frameworks-aurora/base/core/proto/incident.proto", "syntax = \"proto2\";\n")
	mk("packages-aurora/modules/StatsD/statsd/src/statsd_config.proto", "syntax = \"proto2\";\n")
	mk("frameworks-aurora/base/core/proto/sensor.proto", "syntax = \"proto2\";\n")
	// …but NOT this one (an un-forked subtree that must keep its stock reference).
	mk("frameworks/proto_logging/stats/atoms.proto", "syntax = \"proto2\";\n")

	mk("frameworks-aurora/base/tool/a.proto", strings.Join([]string{
		`import "frameworks/base/core/proto/incident.proto";`,              // relocate
		`import "packages/modules/StatsD/statsd/src/statsd_config.proto";`, // relocate — BOTH ROOTS
		`import "frameworks/proto_logging/stats/atoms.proto";`,             // keep stock — not forked
	}, "\n")+"\n")
	mk("frameworks-aurora/base/tool/b.cpp", strings.Join([]string{
		`#include "frameworks/base/core/proto/incident.pb.h"`,  // relocate via .proto producer
		`#include <frameworks/base/core/proto/sensor.proto.h>`, // relocate via .proto producer
		`#include "frameworks/proto_logging/stats/atoms.pb.h"`, // keep stock — producer not forked
	}, "\n")+"\n")

	_, moved, kept := runRelocateStockSourcePaths(LaneConfig{Name: lane}, root)
	// 2 .proto imports + 2 generated-header includes relocate; 2 references have no lane
	// producer and correctly stay stock. (First written as 5 — the assertion was wrong, not the
	// code. Checking the fixture against the reader's actual output before suspecting the
	// implementation is now 7-for-7 across this codebase.)
	if moved != 4 {
		t.Errorf("moved = %d, want 4", moved)
	}
	if kept != 2 {
		t.Errorf("kept (correctly left stock) = %d, want 2", kept)
	}
	proto, _ := os.ReadFile(filepath.Join(root, "frameworks-aurora/base/tool/a.proto"))
	for _, want := range []string{
		`import "frameworks-aurora/base/core/proto/incident.proto";`,
		`import "packages-aurora/modules/StatsD/statsd/src/statsd_config.proto";`,
		`import "frameworks/proto_logging/stats/atoms.proto";`, // untouched
	} {
		if !strings.Contains(string(proto), want) {
			t.Errorf(".proto missing %q\n--- got ---\n%s", want, proto)
		}
	}
	cpp, _ := os.ReadFile(filepath.Join(root, "frameworks-aurora/base/tool/b.cpp"))
	for _, want := range []string{
		`#include "frameworks-aurora/base/core/proto/incident.pb.h"`,
		`#include <frameworks-aurora/base/core/proto/sensor.proto.h>`, // angle form preserved
		`#include "frameworks/proto_logging/stats/atoms.pb.h"`,        // untouched
	} {
		if !strings.Contains(string(cpp), want) {
			t.Errorf(".cpp missing %q\n--- got ---\n%s", want, cpp)
		}
	}
}

// TestDiscoverLaneAllowlists proves the discovery RULE, the ownership exclusion, and idempotency.
// A fixed list of known sites would be a false promise — the next plugin to add one would not be
// on it — so the predicate is "a .go holding a <root>-<existing-lane>/ literal IS an allowlist".
func TestDiscoverLaneAllowlists(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, body string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	os.MkdirAll(filepath.Join(root, "frameworks-holo"), 0o755) // an existing lane
	os.MkdirAll(filepath.Join(root, "frameworks-aurora"), 0o755)

	mk("external/icu/build/icu.go", "package icu\n\nvar allow = []string{\n\t\"packages-holo/modules/RuntimeI18n/apex/\",\n}\n")
	// Owned by a dedicated patcher — must NOT be discovered.
	mk("build/soong/ui/build/finder.go", "package build\n\nvar x = []string{\n\t\"frameworks-holo/base\",\n}\n")
	// Archived copy — not live infrastructure.
	mk("_snapshots/old/finder.go", "package old\n\nvar x = []string{\n\t\"frameworks-holo/base\",\n}\n")
	// Already registers the new lane — idempotent, nothing to add.
	mk("build/soong/java/sdk.go", "package java\n\nvar y = []string{\n\t\"frameworks-holo/base\",\n\t\"frameworks-aurora/base\",\n}\n")

	hits := discoverLaneAllowlists(root, "aurora")
	if len(hits) != 1 {
		var got []string
		for _, h := range hits {
			got = append(got, h.rel)
		}
		t.Fatalf("discovered %d site(s) %v, want exactly 1 (external/icu/build/icu.go)", len(hits), got)
	}
	if filepath.ToSlash(hits[0].rel) != "external/icu/build/icu.go" {
		t.Errorf("discovered %q, want external/icu/build/icu.go", hits[0].rel)
	}
	if len(hits[0].adds) != 1 || hits[0].adds[0] != "packages-aurora/modules/RuntimeI18n/apex/" {
		t.Errorf("adds = %v, want [packages-aurora/modules/RuntimeI18n/apex/]", hits[0].adds)
	}
}

// TestPatchLaneAllowlistFile_RefusesUnparseable pins the safety property: the patch is AST-verified
// before AND after, so a result that would not compile is refused rather than written.
func TestPatchLaneAllowlistFile_RefusesUnparseable(t *testing.T) {
	good := []byte("package p\n\nvar allow = []string{\n\t\"packages-holo/modules/X/\",\n}\n")
	hit := laneAllowlistHit{rel: "p.go", adds: []string{"packages-aurora/modules/X/"}, model: "packages-holo/modules/X/"}
	out, changed, err := patchLaneAllowlistFile(good, hit, "aurora")
	if err != nil || !changed {
		t.Fatalf("valid input: changed=%v err=%v", changed, err)
	}
	if !strings.Contains(string(out), `"packages-aurora/modules/X/",`) {
		t.Errorf("insertion missing:\n%s", out)
	}
	if _, _, err := patchLaneAllowlistFile([]byte("package p\nvar = {{{\n"), hit, "aurora"); err == nil {
		t.Error("expected a pre-parse refusal on unparseable Go, got nil")
	}
}

// TestLaneToolFixes_MatchOrSkip covers the lane-CREATED defect repairs. These are byte-exact
// match-or-skip: on AOSP version drift the source no longer matches and the fix is reported as
// skipped rather than guessed at.
func TestLaneToolFixes_MatchOrSkip(t *testing.T) {
	root := t.TempDir()
	rel := "base/tools/streaming_proto/cpp/main.cpp"
	p := filepath.Join(root, "frameworks-aurora", rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("void f() {\n    header = replace_string(header, '.', '_') + \"_stream_h\";\n}\n"), 0o644)

	if n := runFixLaneCreatedDefects(LaneConfig{Name: "aurora"}, root); n != 1 {
		t.Fatalf("applied = %d, want 1", n)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "replace_string(header, '-', '_')") {
		t.Errorf("hyphen sanitisation missing:\n%s", b)
	}
	// Idempotent: a second run must not double-apply.
	if n := runFixLaneCreatedDefects(LaneConfig{Name: "aurora"}, root); n != 0 {
		t.Errorf("second run applied = %d, want 0 (idempotent)", n)
	}
	// Version drift: source not in the known form → skipped, never guessed.
	os.WriteFile(p, []byte("void f() { /* upstream rewrote this */ }\n"), 0o644)
	if n := runFixLaneCreatedDefects(LaneConfig{Name: "aurora"}, root); n != 0 {
		t.Errorf("drifted source applied = %d, want 0 (match-or-skip)", n)
	}
}

// TestRequalifyPath_PodsCarveOut pins the finder-mirroring rule. A namespace-declaring lane bp is
// DROPPED by apply<Lane>BpRoutes unless it is a decomposition pod, so a //label into a pod must
// name the LANE and a label into any other namespace dir must stay STOCK. Verified against the
// live routing receipt on holo2test: 18 namespace bps, 5 pods loaded, 13 dropped. Wrong in either
// direction is a build error.
func TestRequalifyPath_PodsCarveOut(t *testing.T) {
	root := t.TempDir()
	nsBp := "soong_namespace {\n}\n"
	for _, d := range []string{
		"frameworks-aurora/libs/native_bridge_support/android_api/libc",             // namespace, NOT a pod → stay stock
		"frameworks-aurora/base/packages/SystemUI/pods/com/android/systemui/retail", // namespace + pod → lane
		"frameworks-aurora/base/core/res",                                           // no namespace → lane
	} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
		os.WriteFile(filepath.Join(root, d, "Android.bp"), []byte(nsBp), 0o644)
	}
	os.WriteFile(filepath.Join(root, "frameworks-aurora/base/core/res/Android.bp"), []byte("// plain\n"), 0o644)

	laneMap := map[string]string{"frameworks": "frameworks-aurora"}
	cache := map[string]bool{}
	cases := []struct{ in, want string }{
		{"//frameworks/libs/native_bridge_support/android_api/libc:defaults",
			"//frameworks/libs/native_bridge_support/android_api/libc:defaults"}, // namespace, not pod → STOCK
		{"//frameworks/base/packages/SystemUI/pods/com/android/systemui/retail:impl",
			"//frameworks-aurora/base/packages/SystemUI/pods/com/android/systemui/retail:impl"}, // pod → LANE
		{"//frameworks/base/core/res:framework-res",
			"//frameworks-aurora/base/core/res:framework-res"}, // no namespace → LANE
	}
	for _, c := range cases {
		if got := requalifyLabel(c.in, root, laneMap, cache, false, ""); got != c.want {
			t.Errorf("requalifyLabel(%q)\n  got  %q\n  want %q", c.in, got, c.want)
		}
	}
}

// TestDiscoverNeverAllowSites_FirstLane pins the first-lane gap: a plugin allowlist is found by its
// AddNeverAllowRules call even when NO lane literal exists yet, and PatchNeverallowLanePaths then
// mirrors each stock path onto the lane. Idempotent on a second pass. (android-17 icu.go case.)
func TestDiscoverNeverAllowSites_FirstLane(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, body string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	icu := "package icu\n\nimport \"android/soong/android\"\n\nfunc init() {\n\thost_allowlist := []string{\n\t\t\"external/icu/\",\n\t\t\"frameworks/base/libs/hwui\",\n\t\t\"packages/modules/RuntimeI18n/apex/\",\n\t}\n\tandroid.AddNeverAllowRules(android.NeverAllow().InDirectDeps(\"libandroidicu\").NotIn(host_allowlist...))\n}\n"
	mk("external/icu/build/icu.go", icu)
	// build/soong is owned by the dedicated neverallow.go patcher — must NOT be discovered here.
	mk("build/soong/android/neverallow.go", "package android\n\nfunc init() { AddNeverAllowRules(NeverAllow().NotIn(\"frameworks/base\")) }\n")
	// A plugin with no stock path in it is not a lane concern.
	mk("art/build/art.go", "package art\n\nimport \"android/soong/android\"\n\nfunc init() { android.AddNeverAllowRules(android.NeverAllow().NotIn(\"art/\")) }\n")
	// Test files and snapshots are skipped.
	mk("external/icu/build/icu_test.go", icu)
	mk("_snapshots/x/icu.go", icu)

	hits := discoverNeverAllowSites(root)
	if len(hits) != 1 || filepath.ToSlash(hits[0]) != "external/icu/build/icu.go" {
		t.Fatalf("discovered %v, want exactly [external/icu/build/icu.go]", hits)
	}
	out, changed, err := PatchNeverallowLanePaths([]byte(icu), "holo")
	if err != nil || !changed {
		t.Fatalf("patch: changed=%v err=%v", changed, err)
	}
	for _, want := range []string{"\"frameworks-holo/base/libs/hwui\"", "\"packages-holo/modules/RuntimeI18n/apex/\""} {
		if !strings.Contains(string(out), want) {
			t.Errorf("patched icu.go lacks %s:\n%s", want, out)
		}
	}
	if strings.Contains(string(out), "\"external-holo/") {
		t.Errorf("external/ must not be mirrored (only frameworks/ and packages/):\n%s", out)
	}
	if _, again, _ := PatchNeverallowLanePaths(out, "holo"); again {
		t.Errorf("second pass changed the file — not idempotent")
	}
}
