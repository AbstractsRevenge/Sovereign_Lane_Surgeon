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
	"strings"
	"testing"
)

const otherLaneStub = `package build

import "strings"

func isOtherLaneBp(bp string) bool {
	for _, comp := range strings.Split(bp, "/") {
		if strings.HasSuffix(comp, "-nexusm") ||
			strings.HasSuffix(comp, "-nexus") ||
			strings.HasSuffix(comp, "-product") {
			return true
		}
	}
	return false
}

func isOtherLaneBpForNexusM(bp string) bool {
	if strings.HasPrefix(bp, "external/kotlinc-holo/") {
		return false
	}
	for _, comp := range strings.Split(bp, "/") {
		if strings.HasSuffix(comp, "-holo") ||
			strings.HasSuffix(comp, "-nexus") {
			return true
		}
	}
	return false
}
`

// TestAppendSuffixToOtherLaneFunc: append a new suffix to an OR-chain; result is gofmt-clean
// (the key indentation proof), parses, contains the new suffix, idempotent.
func TestAppendSuffixToOtherLaneFunc(t *testing.T) {
	out, changed, err := appendSuffixToOtherLaneFunc([]byte(otherLaneStub), "isOtherLaneBp", "-aurora")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if !strings.Contains(string(out), `strings.HasSuffix(comp, "-aurora")`) {
		t.Errorf("new suffix not appended:\n%s", out)
	}
	// gofmt-idempotence: the spliced result must ALREADY be gofmt-clean (proves indentation).
	formatted, err := format.Source(out)
	if err != nil {
		t.Fatalf("result not valid Go: %v", err)
	}
	if string(formatted) != string(out) {
		t.Errorf("spliced result is NOT gofmt-clean:\n--- got ---\n%s\n--- gofmt ---\n%s", out, formatted)
	}
	// idempotent
	out2, changed2, _ := appendSuffixToOtherLaneFunc(out, "isOtherLaneBp", "-aurora")
	if changed2 || string(out2) != string(out) {
		t.Error("second append should be a no-op")
	}
}

// TestPatchExistingOtherLaneFuncs: both existing other-lane funcs get the new suffix; the new
// lane's OWN func (if present) is skipped; result gofmt-clean.
func TestPatchExistingOtherLaneFuncs(t *testing.T) {
	out, changed, err := patchExistingOtherLaneFuncs([]byte(otherLaneStub), "aurora", "Aurora")
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 2 {
		t.Errorf("expected 2 funcs changed, got %v", changed)
	}
	// both funcs now reference -aurora
	if strings.Count(string(out), `strings.HasSuffix(comp, "-aurora")`) != 2 {
		t.Errorf("expected -aurora in both funcs:\n%s", out)
	}
	formatted, _ := format.Source(out)
	if string(formatted) != string(out) {
		t.Error("patched result not gofmt-clean")
	}
	// kotlinc-holo carve-out preserved in the nexusm func
	if !strings.Contains(string(out), `strings.HasPrefix(bp, "external/kotlinc-holo/")`) {
		t.Error("kotlinc-holo carve-out lost")
	}
}

// --- v0.3.0: self-drop exclusion + neverallow lane paths ---

func TestExcludeLaneFromStockDrop(t *testing.T) {
	const stub = `package build

func x(ctx Context, config Config, androidBps []string) {
	if !isNexusmLane(config) && !isProductProduct(config) {
		androidBps = dropNonHoloLaneBps(androidBps)
	}
	if len(androidBps) == 0 {
		ctx.Fatalf("No Android.bp found")
	}
}
`
	out, ch, err := excludeLaneFromStockDrop([]byte(stub), "Holotest")
	if err != nil {
		t.Fatal(err)
	}
	if !ch {
		t.Fatal("expected the guard to be amended")
	}
	if !strings.Contains(string(out), `!isProductProduct(config) && !isHolotestLane(config) {`) {
		t.Errorf("splice landed wrong:\n%s", out)
	}
	out2, ch2, err := excludeLaneFromStockDrop(out, "Holotest")
	if err != nil {
		t.Fatal(err)
	}
	if ch2 || strings.Count(string(out2), "isHolotestLane(config)") != 1 {
		t.Error("second run must be a no-op")
	}
}

// A finder with no stock-drop guard has nothing to exclude the lane from. It must report that
// rather than corrupt anything — the caller soft-skips.
func TestExcludeLaneFromStockDropNoGuard(t *testing.T) {
	const stub = `package build

func x(androidBps []string) { _ = androidBps }
`
	if _, _, err := excludeLaneFromStockDrop([]byte(stub), "Holotest"); err == nil {
		t.Error("expected an error naming the missing guard")
	}
}

// The rule is "every stock allowlist path gets a lane twin" — covering a named []string var and
// an inline NotIn(...) arg list, the two shapes neverallow.go actually uses.
func TestPatchNeverallowLanePaths(t *testing.T) {
	const stub = `package android

func rules() {
	javaDeviceForHostProjectsAllowedList := []string{
		"external/guava",
		"frameworks/base/ravenwood",
		"frameworks/layoutlib",
	}
	_ = javaDeviceForHostProjectsAllowedList
	_ = NeverAllow().NotIn("frameworks/native/libs/binder/ndk").Because("x")
}
`
	out, ch, err := PatchNeverallowLanePaths([]byte(stub), "holotest")
	if err != nil {
		t.Fatal(err)
	}
	if !ch {
		t.Fatal("expected changes")
	}
	s := string(out)
	for _, want := range []string{
		`"frameworks-holotest/base/ravenwood"`,
		`"frameworks-holotest/layoutlib"`,
		`"frameworks-holotest/native/libs/binder/ndk"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in:\n%s", want, s)
		}
	}
	// non-frameworks/packages entries are left alone
	if strings.Contains(s, "external-holotest") {
		t.Error("must only mirror frameworks/ and packages/ roots")
	}
	// idempotent
	out2, ch2, err := PatchNeverallowLanePaths(out, "holotest")
	if err != nil {
		t.Fatal(err)
	}
	if ch2 || strings.Count(string(out2), `"frameworks-holotest/layoutlib"`) != 1 {
		t.Error("second run must be a no-op")
	}
}

// A lane that was the FIRST on the tree has no component loop in its isOtherLaneBpFor<X> func —
// the generator omits it, because a loop with an empty body leaves `comp` declared-and-unused.
// Adding a SECOND lane must seed the loop rather than fail: both lanes keep stock module names, so
// a first lane blind to the second sees its bps and collides. (Found seeding holo2 beside holo.)
func TestAppendSuffixSeedsLoopWhenNoChainExists(t *testing.T) {
	src := []byte(`package build

import "strings"

func isOtherLaneBpForHolo(bp string) bool {
	if strings.HasPrefix(bp, "external/kotlinc-holo/") {
		return false
	}
	return false
}
`)
	out, changed, err := appendSuffixToOtherLaneFunc(src, "isOtherLaneBpForHolo", "-holo2")
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	if !changed {
		t.Fatal("reported no change")
	}
	got := string(out)
	for _, want := range []string{
		`for _, comp := range strings.Split(bp, "/") {`,
		`if strings.HasSuffix(comp, "-holo2") {`,
		"return true",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	// the pre-existing guard stays, and stays FIRST
	if i, j := strings.Index(got, "kotlinc-holo"), strings.Index(got, "comp"); i < 0 || j < 0 || i > j {
		t.Fatalf("existing guard lost or reordered:\n%s", got)
	}
	// and it is idempotent
	again, changed2, err := appendSuffixToOtherLaneFunc(out, "isOtherLaneBpForHolo", "-holo2")
	if err != nil || changed2 || string(again) != got {
		t.Fatalf("not idempotent: changed=%v err=%v", changed2, err)
	}
	// a THIRD lane then extends the chain the seed created, rather than seeding again
	out3, changed3, err := appendSuffixToOtherLaneFunc(out, "isOtherLaneBpForHolo", "-holo3")
	if err != nil || !changed3 {
		t.Fatalf("third lane: changed=%v err=%v", changed3, err)
	}
	if strings.Count(string(out3), "for _, comp := range") != 1 {
		t.Fatalf("third lane seeded a second loop:\n%s", string(out3))
	}
}
