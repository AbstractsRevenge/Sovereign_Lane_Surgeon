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
	"strings"
	"testing"
)

// a realistic slice of compatibility.mk around the license-metadata loop (stock form).
const compatStockFixture = `# Copy license metadata
$(call declare-copy-target-license-metadata,$(out_dir)/$(notdir $(test_suite_jdk)),$(test_suite_jdk))
$(foreach t,$(test_tools) $(test_suite_prebuilt_tools),\
  $(eval _dst := $(out_dir)/tools/$(notdir $(t)))\
  $(if $(strip $(ALL_TARGETS.$(t).META_LIC)),\
    $(call declare-copy-target-license-metadata,$(_dst),$(t)),\
    $(warning $(t) has no license metadata)\
  )\
)
test_copied_tools := $(foreach t,$(test_tools) $(test_suite_prebuilt_tools), $(out_dir)/tools/$(notdir $(t)))
`

func TestPatchCompatibilityMk(t *testing.T) {
	out, changed, found, err := patchCompatibilityMk([]byte(compatStockFixture))
	if err != nil {
		t.Fatal(err)
	}
	if !changed || !found {
		t.Fatalf("expected changed+found on stock input (changed=%v found=%v)", changed, found)
	}
	s := string(out)
	if !strings.Contains(s, compatMkBackfillMarker) {
		t.Errorf("patched output missing back-fill marker:\n%s", s)
	}
	if strings.Contains(s, "has no license metadata)\\") {
		t.Errorf("patched output still has the $(warning ...) stock line:\n%s", s)
	}
	// context outside the block is untouched
	if !strings.Contains(s, "test_copied_tools := ") || !strings.Contains(s, "# Copy license metadata") {
		t.Error("surrounding lines should be preserved")
	}

	// idempotent: patching the patched form is a no-op
	out2, changed2, found2, _ := patchCompatibilityMk(out)
	if changed2 || !found2 || string(out2) != string(out) {
		t.Errorf("second patch should be a found no-op (changed=%v found=%v)", changed2, found2)
	}
}

func TestUnpatchCompatibilityMkRoundTrip(t *testing.T) {
	patched, _, _, err := patchCompatibilityMk([]byte(compatStockFixture))
	if err != nil {
		t.Fatal(err)
	}
	back, changed, err := unpatchCompatibilityMk(patched)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected reversal to change")
	}
	if string(back) != compatStockFixture {
		t.Errorf("round-trip not byte-identical to stock:\n--- got ---\n%s\n--- want ---\n%s", back, compatStockFixture)
	}
	// reversing stock (unpatched) is a no-op
	if _, ch, _ := unpatchCompatibilityMk([]byte(compatStockFixture)); ch {
		t.Error("unpatch of stock should be a no-op")
	}
}

// version-drift: a tree whose block doesn't match the known stock form is SKIPPED, not corrupted.
func TestPatchCompatibilityMkVersionDrift(t *testing.T) {
	drift := "# Copy license metadata\n$(some_other_loop_shape_from_a_different_aosp_version)\n"
	out, changed, found, err := patchCompatibilityMk([]byte(drift))
	if err != nil {
		t.Fatal(err)
	}
	if changed || found {
		t.Errorf("drift input should be skipped (changed=%v found=%v)", changed, found)
	}
	if string(out) != drift {
		t.Error("drift input must be returned unchanged")
	}
}
