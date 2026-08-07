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

// TestDeprefixRenameAndRepoint: the Model-A de-prefix — renameAndRepointBp with a de-prefix map
// rewrites BOTH the java_defaults definition name AND a consumer's defaults:[] reference in lockstep,
// so a shared-infra module the lane wrongly prefixed becomes keep-name and resolves for stock
// consumers. Uses lane="" (de-prefix never sets aidl owner:).
func TestDeprefixRenameAndRepoint(t *testing.T) {
	const src = `java_defaults {
    name: "Nexusmwmshell_defaults",
    static_libs: ["some-lib"],
}

android_app {
    name: "NexusMCarSystemUI",
    defaults: ["Nexusmwmshell_defaults"],
    static_libs: ["Nexusmwmshell_defaults"],
}
`
	// The de-prefix map cmdRenameModule builds from -deprefix Nexusm -modules wmshell_defaults.
	rename := map[string]string{"Nexusmwmshell_defaults": "wmshell_defaults"}
	out, changed, err := renameAndRepointBp([]byte(src), rename, "")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	got := string(out)
	if strings.Contains(got, "Nexusmwmshell_defaults") {
		t.Errorf("prefixed name survived de-prefix:\n%s", got)
	}
	if !strings.Contains(got, `name: "wmshell_defaults"`) {
		t.Errorf("definition not de-prefixed:\n%s", got)
	}
	// The consumer's identity name (NexusMCarSystemUI) must be PRESERVED — de-prefix touches only the
	// mapped shared-infra module, never the renamed identity app.
	if !strings.Contains(got, `name: "NexusMCarSystemUI"`) {
		t.Errorf("identity app name wrongly changed:\n%s", got)
	}
	// Both refs (defaults + static_libs) repointed to keep-name.
	if strings.Count(got, `"wmshell_defaults"`) != 3 { // 1 def + 2 refs
		t.Errorf("expected 3 keep-name occurrences (def + 2 refs), got:\n%s", got)
	}
}

// TestDeprefixIdempotent: a tree already keep-name (no map key present) is untouched.
func TestDeprefixIdempotent(t *testing.T) {
	const src = `java_defaults {
    name: "wmshell_defaults",
    static_libs: ["some-lib"],
}
`
	rename := map[string]string{"Nexusmwmshell_defaults": "wmshell_defaults"}
	out, changed, err := renameAndRepointBp([]byte(src), rename, "")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("expected no change on an already keep-name tree")
	}
	if string(out) != src {
		t.Error("content mutated on a no-op de-prefix")
	}
}
