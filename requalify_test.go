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
	parser "github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/internal/blueprint/parser"
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

// --- lane-sourced fork (v0.3.0) ---

func TestLaneDirForAcceptsLaneRoot(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"frameworks/base", "frameworks-holotest/base", true},
		{"packages/apps/Dialer", "packages-holotest/apps/Dialer", true},
		{"frameworks-holo/base", "frameworks-holotest/base", true}, // lane SOURCE
		{"packages-holo", "packages-holotest", true},               // bare lane root
		{"external/kotlin", "", false},                             // not a lane root
		{"frameworksfoo/base", "", false},                          // whole-segment match only
	}
	for _, c := range cases {
		got, ok := laneDirFor(c.in, "holotest")
		if got != c.want || ok != c.ok {
			t.Errorf("laneDirFor(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestRequalifyLaneToLaneForms(t *testing.T) {
	m := map[string]string{"frameworks-holo": "frameworks-holotest", "packages-holo": "packages-holotest"}
	cache := map[string]bool{}
	// qualified labels (force=true so no on-disk check is needed)
	for _, c := range []struct{ in, want string }{
		{"//frameworks-holo/base/services", "//frameworks-holotest/base/services"},
		{"//packages-holo/modules/Permission:__subpackages__", "//packages-holotest/modules/Permission:__subpackages__"},
		{"//frameworks/base/core", "//frameworks/base/core"}, // stock untouched when mapping from a lane
		{"//frameworks-holo2/x", "//frameworks-holo2/x"},     // whole-segment: no overmatch
	} {
		if got := requalifyLabel(c.in, "/nonexistent", m, cache, true, ""); got != c.want {
			t.Errorf("requalifyLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// embedded paths — the build-blocking $(location) case, and flag fragments
	for _, c := range []struct{ in, want string }{
		{"x $(location //frameworks-holo/base/core/java:protolog-groups) y",
			"x $(location //frameworks-holotest/base/core/java:protolog-groups) y"},
		{"--header-filter=^.*frameworks-holo/native/libs/binder/.*.h$",
			"--header-filter=^.*frameworks-holotest/native/libs/binder/.*.h$"},
		{"-Aroom.schemaLocation=packages-holo/modules/X/schemas",
			"-Aroom.schemaLocation=packages-holotest/modules/X/schemas"},
		{"nothing to see here", "nothing to see here"},
	} {
		if got := requalifyEmbedded(c.in, m); got != c.want {
			t.Errorf("requalifyEmbedded(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRequalifyBarePathIsConservative(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "frameworks-holotest", "rs"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := map[string]string{"frameworks-holo": "frameworks-holotest"}
	cache := map[string]bool{}
	// target exists in the lane => rewrite
	if got := requalifyBarePath("frameworks-holo/rs", dir, m, cache, false, ""); got != "frameworks-holotest/rs" {
		t.Errorf("existing target: got %q", got)
	}
	// target absent => keep the original rather than pointing at nothing
	if got := requalifyBarePath("frameworks-holo/nope", dir, m, cache, false, ""); got != "frameworks-holo/nope" {
		t.Errorf("absent target should be left alone: got %q", got)
	}
}

// A lane-sourced clone keeps its source lane's soong_config namespace, which is NOT a path — so
// no path rewrite reaches it, and a condition naming a namespace the new lunch never sets quietly
// takes the default (stock) branch of the lane's own lane-aware select.
func TestSoongConfigRenamer(t *testing.T) {
	r := soongConfigRenamer("holo", "holotest")
	for _, c := range []struct{ in, want string }{
		{"holo_framework_routing", "holotest_framework_routing"},
		{"enable_holo_res", "enable_holotest_res"},
		{"holo_package_routing", "holotest_package_routing"},
		{"enable_holo_java_overrides", "enable_holotest_java_overrides"},
		{"unrelated_namespace", "unrelated_namespace"},
		{"holograph_thing", "holograph_thing"}, // whole-token: never a substring match
	} {
		if got := r(c.in); got != c.want {
			t.Errorf("soongConfigRenamer(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if soongConfigRenamer("holo", "holo") != nil {
		t.Error("same source and target must be a no-op renamer")
	}
}

// TestRequalifyEmbeddedLabels pins the whole-root-fork case: a qualified label inside a genrule cmd
// string is repointed iff its lane parallel exists (the finder drops the stock bp then), and is left
// alone otherwise. The interior-segment trap does not apply: "$(location //" anchors a label start.
func TestRequalifyEmbeddedLabels(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "packages-holo", "services", "x", "utils", "gen"), 0o755)
	os.WriteFile(filepath.Join(root, "packages-holo", "services", "x", "utils", "gen", "Android.bp"), []byte("// lane\n"), 0o644)
	m := map[string]string{"frameworks": "frameworks-holo", "packages": "packages-holo"}
	cache := map[string]bool{}
	for _, c := range []struct{ in, want string }{
		{"$(location //packages/services/x/utils/gen:gen) $(location other) $(out) $(in)",
			"$(location //packages-holo/services/x/utils/gen:gen) $(location other) $(out) $(in)"},
		{"$(locations //packages/services/x/utils/gen) more", "$(locations //packages-holo/services/x/utils/gen) more"},
		{"$(location //packages/services/unforked/tool:tool)", "$(location //packages/services/unforked/tool:tool)"}, // no lane parallel: stock stays
		{"//frameworks/base/packages/SystemUI/aconfig:flags", "//frameworks/base/packages/SystemUI/aconfig:flags"},   // not embedded: untouched here
		{"plain cmd $(out)", "plain cmd $(out)"},
	} {
		if got := requalifyEmbeddedLabels(c.in, root, m, cache, false, ""); got != c.want {
			t.Errorf("requalifyEmbeddedLabels(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

// TestMirrorExportHeaders pins the paired-property rule: an export_*_headers entry must carry the
// exact string of its library entry, so a qualified shared_libs label mirrors onto the bare export
// entry (Soong's inList check is textual). Nested library lists are seen; unrelated names and an
// already-mirrored file are untouched.
func TestMirrorExportHeaders(t *testing.T) {
	src := "cc_library {\n    name: \"x\",\n    target: {\n        android: {\n            shared_libs: [\"//frameworks-holo/av/media/module/bufferpool/2.0:libstagefright_bufferpool@2.0.1\", \"libui\"],\n        },\n    },\n    export_shared_lib_headers: [\"libstagefright_bufferpool@2.0.1\", \"libui\"],\n    static_libs: [\"//a/b:libfoo\"],\n    export_static_lib_headers: [\"libfoo\", \"libbar\"],\n}\n"
	file, errs := parser.Parse("", strings.NewReader(src))
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	m := file.Defs[0].(*parser.Module)
	if !mirrorExportHeaders(m) {
		t.Fatal("expected a change")
	}
	lists := map[string][]string{}
	for _, p := range m.Properties {
		if l, ok := p.Value.(*parser.List); ok {
			for _, el := range l.Values {
				if s, ok := el.(*parser.String); ok {
					lists[p.Name] = append(lists[p.Name], s.Value)
				}
			}
		}
	}
	want := map[string][]string{
		"export_shared_lib_headers": {"//frameworks-holo/av/media/module/bufferpool/2.0:libstagefright_bufferpool@2.0.1", "libui"},
		"export_static_lib_headers": {"//a/b:libfoo", "libbar"},
	}
	for prop, w := range want {
		if strings.Join(lists[prop], ",") != strings.Join(w, ",") {
			t.Errorf("%s = %v, want %v", prop, lists[prop], w)
		}
	}
	if mirrorExportHeaders(m) {
		t.Errorf("second pass changed the module — not idempotent")
	}
	// Reverse direction: qualified export against a bare library entry (a merge that took
	// upstream's shared_libs) — the library entry takes the qualified form.
	rev := "cc_library {\n    name: \"y\",\n    shared_libs: [\"libstagefright_bufferpool@2.0.1\"],\n    export_shared_lib_headers: [\"//frameworks-holo/av/media/module/bufferpool/2.0:libstagefright_bufferpool@2.0.1\"],\n}\n"
	f2, errs2 := parser.Parse("", strings.NewReader(rev))
	if len(errs2) > 0 {
		t.Fatal(errs2[0])
	}
	m2 := f2.Defs[0].(*parser.Module)
	if !mirrorExportHeaders(m2) {
		t.Fatal("reverse case: expected a change")
	}
	for _, p := range m2.Properties {
		if p.Name == "shared_libs" {
			if s := p.Value.(*parser.List).Values[0].(*parser.String).Value; !strings.HasPrefix(s, "//") {
				t.Errorf("reverse case: shared_libs entry not qualified: %s", s)
			}
		}
	}
}
