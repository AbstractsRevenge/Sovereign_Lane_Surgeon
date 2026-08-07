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
