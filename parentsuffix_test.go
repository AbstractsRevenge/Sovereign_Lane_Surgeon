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

func TestMapPrefixedPathSuffixesOnlyImmediateParents(t *testing.T) {
	c := deriveLane("holo2", false, nil, false, false, "")
	c.ParentDirSuffix = "_holo2"
	for in, want := range map[string]string{
		"frameworks-holo/base/packages/SystemUI/src/com/android": "frameworks-holo/base/packages/SystemUI_holo2/src/com/android",
		"packages-holo/apps/Settings/src/com/android":            "packages-holo/apps/Settings_holo2/src/com/android",
		"frameworks-holo/base/core/res":                          "frameworks-holo/base/core/res",
		"packages-holo/modules/Permission":                       "packages-holo/modules/Permission",
	} {
		if got := mapPrefixedPath(c, in); got != want {
			t.Errorf("mapPrefixedPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParentSuffixRenamesIncludeLaneSourcePaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "frameworks-holo2", "base", "packages", "SystemUI_holo2"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := deriveLane("holo2", false, nil, false, false, "")
	c.FromLane = "holo"
	c.ParentDirSuffix = "_holo2"
	renames := parentSuffixRenames(c, root)
	want := map[parentPathRename]bool{
		{from: "frameworks-holo2/base/packages/SystemUI", to: "frameworks-holo2/base/packages/SystemUI_holo2"}: true,
		{from: "frameworks-holo/base/packages/SystemUI", to: "frameworks-holo2/base/packages/SystemUI_holo2"}:  true,
	}
	for _, rename := range renames {
		delete(want, rename)
	}
	if len(want) != 0 {
		t.Fatalf("missing target/source parent renames: %#v (got %#v)", want, renames)
	}
}

func TestReplaceBoundedPathDoesNotCorruptSiblingPrefix(t *testing.T) {
	src := []byte(`//frameworks-holo2/base/packages/Settings:lib //frameworks-holo2/base/packages/SettingsLib:lib`)
	got, n := replaceBoundedPath(src,
		"frameworks-holo2/base/packages/Settings",
		"frameworks-holo2/base/packages/Settings_holo2")
	if n != 1 {
		t.Fatalf("replaced %d paths, want 1: %s", n, got)
	}
	if strings.Contains(string(got), "Settings_holo2Lib") {
		t.Fatalf("Settings replacement corrupted SettingsLib: %s", got)
	}
}

func TestRewriteParentSuffixPathsUpdatesRelativeBlueprintPath(t *testing.T) {
	root := t.TempDir()
	targetParent := filepath.Join(root, "frameworks-holo2", "base", "packages", "Vcn_holo2")
	if err := os.MkdirAll(targetParent, 0o755); err != nil {
		t.Fatal(err)
	}
	bp := filepath.Join(root, "frameworks-holo2", "base", "AconfigFlags.bp")
	if err := os.WriteFile(bp, []byte(`filegroup { name: "x", srcs: ["packages/Vcn/framework-b/X.java"] }\n`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := deriveLane("holo2", false, nil, false, false, "")
	c.ParentDirSuffix = "_holo2"
	files, refs := runRewriteParentSuffixPaths(c, root)
	if files != 1 || refs != 1 {
		t.Fatalf("rewrite = %d files, %d refs; want 1, 1", files, refs)
	}
	b, err := os.ReadFile(bp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"packages/Vcn_holo2/framework-b/X.java"`) {
		t.Fatalf("relative parent path was not suffixed: %s", b)
	}
}
