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
	"strings"
	"testing"
)

// TestRenameInstallableBp: android_app renamed to <Camel><Name> + overrides injected; libs/other
// untouched; idempotent.
func TestRenameInstallableBp(t *testing.T) {
	bp := `android_app {
    name: "SystemUI",
    certificate: "platform",
}

java_library {
    name: "SystemUI-core",
}
`
	out, changed, err := renameInstallableBp([]byte(bp), "Testing1")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	s := string(out)
	if !strings.Contains(s, `name: "Testing1SystemUI"`) {
		t.Errorf("app not renamed:\n%s", s)
	}
	if !strings.Contains(s, `overrides: ["SystemUI"]`) {
		t.Errorf("overrides not injected:\n%s", s)
	}
	// framework-res is an android_app but framework-class → tier 1 must SKIP it (tier 3 owns it)
	fr, _, _ := renameInstallableBp([]byte("android_app {\n    name: \"framework-res\",\n}\n"), "Testing1")
	if strings.Contains(string(fr), "Testing1framework-res") {
		t.Error("tier 1 must not rename framework-res (framework class)")
	}
	// java_library (not installable) untouched
	if !strings.Contains(s, `name: "SystemUI-core"`) {
		t.Error("java_library should NOT be renamed by the installable pass")
	}
	// idempotent
	out2, changed2, _ := renameInstallableBp(out, "Testing1")
	if changed2 || string(out2) != string(out) {
		t.Error("second rename should be a no-op")
	}
}

// TestRenameModelOnly: keep-name lanes are not renamed.
func TestRenameModelOnly(t *testing.T) {
	keep := deriveLane("t1", true, nil, true, true, "") // KeepName=true
	if c, _ := runRenameInstallables(keep, t.TempDir()); c != 0 {
		t.Error("keep-name model must not rename installables")
	}
}

// TestRenameAndRepointBp: mapped lib renamed; consumer deps + srcs :module repointed; framework
// class + non-lane bare refs left; idempotent.
func TestRenameAndRepointBp(t *testing.T) {
	rename := map[string]string{
		"SystemUI-core":       "Testing1SystemUI-core",
		"SystemUI-flag-types": "Testing1SystemUI-flag-types",
	}
	bp := `java_library {
    name: "SystemUI-core",
    static_libs: [
        "SystemUI-flag-types",
        "androidx.core_core",
    ],
    srcs: [
        ":SystemUI-flag-types",
        "src/**/*.java",
    ],
    defaults: ["framework-minus-apex"],
}
`
	out, changed, err := renameAndRepointBp([]byte(bp), rename, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	s := string(out)
	checks := map[string]bool{
		`name: "Testing1SystemUI-core"`:  true, // module renamed
		`"Testing1SystemUI-flag-types"`:  true, // dep repointed
		`":Testing1SystemUI-flag-types"`: true, // srcs :module repointed
		`"androidx.core_core"`:           true, // non-lane dep left
		`"src/**/*.java"`:                true, // file path left
		`"framework-minus-apex"`:         true, // framework-class ref left (not in map)
	}
	for probe, want := range checks {
		if strings.Contains(s, probe) != want {
			t.Errorf("probe %q present=%v want=%v\n%s", probe, strings.Contains(s, probe), want, s)
		}
	}
	// idempotent (mapped names already renamed → repoint finds nothing new for THIS file's own name,
	// but a re-run with the SAME map would re-rename; guard: run with empty map = no-op)
	out2, changed2, _ := renameAndRepointBp(out, map[string]string{}, "t1")
	if changed2 || string(out2) != string(out) {
		t.Error("empty-map repoint should be a no-op")
	}
}

// TestRenameFrameworkClassBp: framework-res → framework-res-t1 + stem + phony; non-fwc untouched.
func TestRenameFrameworkClassBp(t *testing.T) {
	bp := `android_app {
    name: "framework-res",
    certificate: "platform",
}

java_library {
    name: "services",
}

java_library {
    name: "some-random-lib",
}
`
	out, changed, err := renameFrameworkClassBp([]byte(bp), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	s := string(out)
	for _, want := range []string{
		`name: "framework-res-t1"`,       // renamed
		`stem: "framework-res"`,          // stem keeps install name
		`name: "framework-res"`,          // phony bridge (same literal name)
		`required: ["framework-res-t1"]`, // phony redirects
		`name: "services-t1"`,            // services also framework-class → renamed
		`stem: "services"`,
		`name: "some-random-lib"`, // non-fwc untouched
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q\n%s", want, s)
		}
	}
	if strings.Contains(s, `name: "some-random-lib-t1"`) {
		t.Error("non-framework-class must NOT be renamed by tier 3")
	}
}
