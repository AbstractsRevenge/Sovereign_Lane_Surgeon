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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRequalify: a forked label is repointed to the lane path; an unforked one stays stock.
func TestRequalify(t *testing.T) {
	root := t.TempDir()
	// lane forked frameworks/base/services + frameworks/base/core (dirs exist) but NOT SystemUI.
	for _, d := range []string{
		"frameworks-testing1/base/services", "frameworks-testing1/base/core",
	} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bp := `java_library {
    name: "services.accessibility",
    static_libs: [
        "//frameworks/base/services:svc-core",
        "//frameworks/base/packages/SystemUI/aconfig:flags",
        "bare_dep",
    ],
    srcs: ["//frameworks/base/core:gen"],
}
`
	p := filepath.Join(root, "frameworks-testing1/base/services/Android.bp")
	os.WriteFile(p, []byte(bp), 0o644)

	cfg := deriveLane("testing1", true, nil, true, true, "")
	changed, failed := runRequalify(cfg, root)
	if failed != 0 {
		t.Fatalf("parse failures: %d", failed)
	}
	if changed != 1 {
		t.Errorf("changed = %d, want 1", changed)
	}
	out, _ := os.ReadFile(p)
	s := string(out)
	// forked targets repointed (form preserved, not bareified)
	if !strings.Contains(s, `"//frameworks-testing1/base/services:svc-core"`) {
		t.Errorf("services label not repointed:\n%s", s)
	}
	if !strings.Contains(s, `"//frameworks-testing1/base/core:gen"`) {
		t.Errorf("core srcs label not repointed:\n%s", s)
	}
	// UNFORKED SystemUI stays stock
	if !strings.Contains(s, `"//frameworks/base/packages/SystemUI/aconfig:flags"`) {
		t.Errorf("unforked SystemUI label must stay stock:\n%s", s)
	}
	// bare dep untouched
	if !strings.Contains(s, `"bare_dep"`) {
		t.Error("bare dep mangled")
	}
}
