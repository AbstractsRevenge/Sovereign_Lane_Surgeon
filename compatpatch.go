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
	"strings"
)

// compatpatch.go — a LANE-INDEPENDENT stock patch the surgeon applies alongside the soong
// lane-routing patches: the license-metadata back-fill in
// build/make/core/tasks/tools/compatibility.mk.
//
// Why every sovereign lane needs it: a full-image build (`m droid superimage`, or any
// compatibility-suite target like `m mts`/`m vts`) evaluates the suite .mk BEFORE the tradefed
// host-tool modules register their license metadata, so the stock loop emits
// "<tool> has no license metadata" and (in a lane build) fails the run. The fix back-fills the
// suite's own license onto any tool whose ALL_TARGETS.<path>.META_LIC is still empty at parse
// time — mirroring the pre-existing $(test_suite_readme) handling in the same file. It is
// off the critical path for a scoped module build (e.g. `m NexusMSystemUI`) but load-bearing
// for the eventual full device image, so the surgeon lands it up front for every adopter.
// (T-directed 2026-07-13; grounded against the real fix at
// build/make/core/tasks/tools/compatibility.mk, verified `m mts` warnings many→0.)
//
// The patch is byte-exact and non-fuzzy: it matches the known stock block once and swaps it,
// or SKIPS with a clear message when the tree's block differs (a different AOSP version) —
// it never guesses at a brace-heavy make construct (HARD RULE 9). Idempotent + reversible.

const compatibilityMkRel = "build/make/core/tasks/tools/compatibility.mk"

// compatMkStockBlock is the stock (pre-patch) license-metadata loop — the exact match target.
// Indentation is spaces (verified), line continuations are literal backslash-newline.
const compatMkStockBlock = `$(foreach t,$(test_tools) $(test_suite_prebuilt_tools),\
  $(eval _dst := $(out_dir)/tools/$(notdir $(t)))\
  $(if $(strip $(ALL_TARGETS.$(t).META_LIC)),\
    $(call declare-copy-target-license-metadata,$(_dst),$(t)),\
    $(warning $(t) has no license metadata)\
  )\
)`

// compatMkPatchedBlock back-fills META_LIC when empty, then declares the copy unconditionally.
const compatMkPatchedBlock = `# Back-fill the suite's own license onto any tool whose ALL_TARGETS.<path>.META_LIC
# has not been registered yet at parse time (mirrors the $(test_suite_readme) handling
# above), then declare the copy. Avoids spurious "has no license metadata" warnings for
# the tradefed host tools whose meta_lic registers after this .mk is evaluated.
$(foreach t,$(test_tools) $(test_suite_prebuilt_tools),\
  $(eval _dst := $(out_dir)/tools/$(notdir $(t)))\
  $(if $(strip $(ALL_TARGETS.$(t).META_LIC)),,$(eval ALL_TARGETS.$(t).META_LIC := $(module_license_metadata)))\
  $(call declare-copy-target-license-metadata,$(_dst),$(t))\
)`

// compatMkBackfillMarker uniquely identifies the patched form (idempotence + reversal probe).
const compatMkBackfillMarker = `$(if $(strip $(ALL_TARGETS.$(t).META_LIC)),,$(eval ALL_TARGETS.$(t).META_LIC := $(module_license_metadata)))`

// patchCompatibilityMk swaps the stock license-metadata loop for the back-filling one.
//
//	changed=true, found=true  → patched now.
//	changed=false, found=true → already patched (idempotent no-op).
//	changed=false, found=false → stock block not in the known form (AOSP-version drift); the
//	                             caller SKIPS and warns rather than guessing.
func patchCompatibilityMk(content []byte) (out []byte, changed, found bool, err error) {
	s := string(content)
	if strings.Contains(s, compatMkBackfillMarker) {
		return content, false, true, nil // already patched
	}
	switch strings.Count(s, compatMkStockBlock) {
	case 0:
		return content, false, false, nil // version drift — caller warns
	case 1:
		return []byte(strings.Replace(s, compatMkStockBlock, compatMkPatchedBlock, 1)), true, true, nil
	default:
		return content, false, true, fmt.Errorf("%s: stock license-metadata block found more than once", compatibilityMkRel)
	}
}

// unpatchCompatibilityMk reverses patchCompatibilityMk byte-for-byte (used by uninstall).
//
//	changed=true  → reverted to stock.
//	changed=false → not patched by us (no marker) → left as-is.
func unpatchCompatibilityMk(content []byte) (out []byte, changed bool, err error) {
	s := string(content)
	if !strings.Contains(s, compatMkPatchedBlock) {
		return content, false, nil
	}
	if strings.Count(s, compatMkPatchedBlock) != 1 {
		return content, false, fmt.Errorf("%s: patched block found more than once", compatibilityMkRel)
	}
	return []byte(strings.Replace(s, compatMkPatchedBlock, compatMkStockBlock, 1)), true, nil
}
