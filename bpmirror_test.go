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

func fakeStockTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"frameworks/base/core/res/Android.bp":             "android_app {\n    name: \"framework-res\",\n    sdk_version: \"core_platform\",\n}\n",
		"frameworks/base/core/res/res/values/strings.xml": "<resources><string name=\"x\">y</string></resources>\n",
		"packages/apps/Settings/Android.bp":               "android_app {\n    name: \"Settings\",\n}\n",
	}
	for rel, c := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestRunMirror: clone two stock subtrees keep-name + write both root namespaces.
func TestRunMirror(t *testing.T) {
	root := fakeStockTree(t)
	cfg := deriveLane("holo", true, nil, true, true, "")
	copied, nsWrote, fatal := runMirror(cfg, root, []string{"frameworks/base/core/res", "packages/apps/Settings"})
	if fatal {
		t.Fatal("runMirror fatal")
	}
	if copied != 3 { // res/Android.bp + res/res/values/strings.xml + Settings/Android.bp
		t.Errorf("copied = %d, want 3", copied)
	}
	if nsWrote != 2 {
		t.Errorf("nsWrote = %d, want 2 (frameworks-holo + packages-holo)", nsWrote)
	}

	// Cloned bp KEEPS the stock module name (keep-name — no rewrite).
	b, err := os.ReadFile(filepath.Join(root, "frameworks-holo/base/core/res/Android.bp"))
	if err != nil {
		t.Fatal("cloned res bp missing:", err)
	}
	if !strings.Contains(string(b), `name: "framework-res"`) {
		t.Error("cloned framework-res bp should KEEP the stock name, got:\n" + string(b))
	}
	// Source file cloned too (full-subtree fork).
	if _, err := os.Stat(filepath.Join(root, "frameworks-holo/base/core/res/res/values/strings.xml")); err != nil {
		t.Error("source file not cloned:", err)
	}
	// Root namespaces.
	fnb, _ := os.ReadFile(filepath.Join(root, "frameworks-holo/Android.bp"))
	if !strings.Contains(string(fnb), "soong_namespace {") || !strings.Contains(string(fnb), "imports: [],") {
		t.Errorf("frameworks-holo root namespace wrong:\n%s", fnb)
	}
	pnb, _ := os.ReadFile(filepath.Join(root, "packages-holo/Android.bp"))
	if !strings.Contains(string(pnb), `imports: ["frameworks-holo"],`) {
		t.Errorf("packages-holo root namespace should import frameworks-holo:\n%s", pnb)
	}
	if !strings.Contains(string(pnb), `default_visibility: ["//visibility:public"]`) {
		t.Error("packages-holo root missing public package block")
	}

	// Idempotent: re-run clones nothing new (no-clobber), namespaces skipped.
	copied2, nsWrote2, _ := runMirror(cfg, root, []string{"frameworks/base/core/res", "packages/apps/Settings"})
	if copied2 != 0 || nsWrote2 != 0 {
		t.Errorf("re-run should be a no-op, got copied=%d nsWrote=%d", copied2, nsWrote2)
	}
}

// TestLaneDirFor: mapping stock subtree → lane parallel.
func TestLaneDirFor(t *testing.T) {
	cases := map[string]string{
		"frameworks/base/core/res": "frameworks-holo/base/core/res",
		"packages/apps/Settings":   "packages-holo/apps/Settings",
		"frameworks":               "frameworks-holo",
	}
	for in, want := range cases {
		got, ok := laneDirFor(in, "holo")
		if !ok || got != want {
			t.Errorf("laneDirFor(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	if _, ok := laneDirFor("external/foo", "holo"); ok {
		t.Error("external/ subtree should be rejected")
	}
}

// TestMirrorSkipsGit: a real subtree carries .git — the mirror must skip it, not abort.
func TestMirrorSkipsGit(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "frameworks", "base")
	if err := os.MkdirAll(filepath.Join(base, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(base, ".git", "config"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(base, "Android.bp"), []byte("cc_library { name: \"libx\" }\n"), 0o644)
	os.WriteFile(filepath.Join(base, "core.java"), []byte("//src\n"), 0o644)

	cfg := deriveLane("testing1", true, nil, true, true, "")
	_, copied, err := mirrorSubtree(cfg, root, "frameworks/base")
	if err != nil {
		t.Fatalf("mirror aborted on .git: %v", err)
	}
	_ = cfg
	// Source files cloned, .git NOT.
	if _, err := os.Stat(filepath.Join(root, "frameworks-testing1", "base", "Android.bp")); err != nil {
		t.Error("Android.bp not cloned")
	}
	if _, err := os.Stat(filepath.Join(root, "frameworks-testing1", "base", ".git")); !os.IsNotExist(err) {
		t.Error(".git was cloned — must be skipped")
	}
	if copied != 2 { // Android.bp + core.java, NOT the .git files
		t.Errorf("copied = %d, want 2 (source only, no .git)", copied)
	}
}

// TestDefaultInfraExcludes: forking frameworks/base auto-excludes infra; keep-name adds SystemUI.
func TestDefaultInfraExcludes(t *testing.T) {
	keep := deriveLane("t1", true, nil, true, true, "")
	keep.Forks = []string{"frameworks/base", "packages/apps/Foo"}
	ex := defaultInfraExcludes(keep)
	for _, want := range []string{"frameworks/base/ravenwood", "frameworks/base/tools/hoststubgen", "frameworks/base/packages/SystemUI"} {
		found := false
		for _, e := range ex {
			if e == want {
				found = true
			}
		}
		if !found {
			t.Errorf("keep-name frameworks/base fork should auto-exclude %q", want)
		}
	}
	// rename model: SystemUI is NOT excluded (it gets renamed instead)
	ren := deriveLane("t1", false, nil, true, true, "")
	ren.Forks = []string{"frameworks/base"}
	for _, e := range defaultInfraExcludes(ren) {
		if e == "frameworks/base/packages/SystemUI" {
			t.Error("rename model must not exclude SystemUI")
		}
	}
	// no frameworks/base fork → no auto-excludes
	noBase := deriveLane("t1", true, nil, true, true, "")
	noBase.Forks = []string{"packages/apps/Foo"}
	if len(defaultInfraExcludes(noBase)) != 0 {
		t.Error("non-frameworks/base fork should have no auto-excludes")
	}
	// mergeDedup
	if got := mergeDedup([]string{"a", "/b"}, []string{"b", "c"}); strings.Join(got, ",") != "a,b,c" {
		t.Errorf("mergeDedup = %v", got)
	}
}

// TestScopeSystemUISrcsBp — the -no-compose SystemUI srcs-scoping transform (Part 2d): scope srcs to
// the re-authored kotlin/** tree, drop Compose static_libs, KEEP non-Compose libs (dagger, androidx.core
// which the lane vendors in-app), and be idempotent.
func TestScopeSystemUISrcsBp(t *testing.T) {
	bp := `android_app {
    name: "FooSystemUI",
    srcs: [
        "src/**/*.java",
        "compose/**/*.kt",
        "kotlin/**/*.kt",
    ],
    static_libs: [
        "dagger2",
        "SystemUI-compose-core",
        "androidx.core_core",
    ],
}`
	out, changed, err := scopeSystemUISrcsBp([]byte(bp))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected a change")
	}
	s := string(out)
	if !strings.Contains(s, `"kotlin/**/*.kt"`) {
		t.Error("srcs must be scoped to kotlin/**/*.kt")
	}
	if strings.Contains(s, "compose/**") || strings.Contains(s, "src/**") {
		t.Error("non-kotlin srcs globs must be dropped")
	}
	if strings.Contains(s, "SystemUI-compose-core") {
		t.Error("Compose static_lib must be dropped")
	}
	if !strings.Contains(s, "dagger2") || !strings.Contains(s, "androidx.core_core") {
		t.Error("non-Compose static_libs (dagger2, androidx.core) must be KEPT (AndroidX is vendored in-app, not dropped)")
	}
	// idempotent
	if _, changed2, _ := scopeSystemUISrcsBp(out); changed2 {
		t.Error("second pass must be a no-op (idempotent)")
	}
}
