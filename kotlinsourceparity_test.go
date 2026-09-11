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

func TestMatchPathGlobBlueprintDoubleStar(t *testing.T) {
	cases := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"**/*.kt", "Root.kt", true},
		{"**/*.kt", "src/nested/Source.kt", true},
		{"src/**/Immutable.kt", "src/Immutable.kt", true},
		{"src/**/Immutable.kt", "src/a/b/Immutable.kt", true},
		{"android/os/*MessageQueue/**/*.kt", "android/os/TestMessageQueue/Foo.kt", true},
		{"android/os/*MessageQueue/**/*.kt", "android/os/TestMessageQueue/nested/Foo.kt", true},
		{"src/**/*.kt", "src/Foo.java", false},
	}
	for _, tc := range cases {
		if got := matchPathGlob(tc.pattern, tc.name); got != tc.want {
			t.Errorf("matchPathGlob(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

func TestRepairKotlinSourceParityBP(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{"intentional/Foo.kt", "generated/Generated.kt", "exact/Excluded.kt"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package test\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	stock := []byte(`
java_library {
    name: "empty-kotlin-glob",
    srcs: ["src/**/*.java"],
}
java_library {
    name: "target-owned-kotlin",
    srcs: ["owned/**/*.kt"],
}
java_library {
    name: "real-lane-kotlin",
}
java_library {
    name: "fully-excluded-kotlin",
}
java_library {
    name: "exact-excluded-kotlin",
}
java_library {
    name: "adservices-shared-common",
    exclude_srcs: ["java/com/android/adservices/shared/common/system/*.java"],
}
`)
	lane := []byte(`
java_library {
    name: "empty-kotlin-glob",
    srcs: ["src/**/*.java", "src/**/*.kt"],
}
java_library {
    name: "target-owned-kotlin",
    srcs: ["owned/**/*.kt"],
}
java_library {
    name: "real-lane-kotlin",
    srcs: ["intentional/**/*.kt"],
}
java_library {
    name: "fully-excluded-kotlin",
    srcs: ["generated/**/*.kt"],
    exclude_srcs: ["generated/**/*.kt", "empty/**/*.kt"],
}
java_library {
    name: "exact-excluded-kotlin",
    srcs: ["exact/Excluded.kt"],
    exclude_srcs: ["exact/Excluded.kt"],
}
java_library {
    name: "adservices-shared-common",
    exclude_srcs: [
        "kotlin/com/android/adservices/shared/common/system/*.java",
        "kotlin/com/android/adservices/shared/common/system/*.kt",
    ],
}
`)

	got, dropped, repointed, err := repairKotlinSourceParityBP(lane, stock, dir)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 5 {
		t.Fatalf("dropped = %d, want 5\n%s", dropped, got)
	}
	if repointed != 1 {
		t.Fatalf("repointed = %d, want 1\n%s", repointed, got)
	}
	out := string(got)
	for _, absent := range []string{`"src/**/*.kt"`, `"empty/**/*.kt"`, `"kotlin/com/android/adservices/shared/common/system/`} {
		if strings.Contains(out, absent) {
			t.Errorf("output still contains %s:\n%s", absent, out)
		}
	}
	for _, present := range []string{`"owned/**/*.kt"`, `"intentional/**/*.kt"`, `"generated/**/*.kt"`, `"java/com/android/adservices/shared/common/system/*.java"`} {
		if !strings.Contains(out, present) {
			t.Errorf("output lost %s:\n%s", present, out)
		}
	}
}

func TestStockParallelBPHolo2Parents(t *testing.T) {
	root := t.TempDir()
	c := LaneConfig{Name: "holo2", ParentDirSuffix: "_holo2"}
	cases := map[string]string{
		"frameworks-holo2/base/packages/SystemUI_holo2/Android.bp":      "frameworks/base/packages/SystemUI/Android.bp",
		"frameworks-holo2/base/packages/SettingsLib_holo2/a/Android.bp": "frameworks/base/packages/SettingsLib/a/Android.bp",
		"frameworks-holo2/base/core/java/Android.bp":                    "frameworks/base/core/java/Android.bp",
		"packages-holo2/apps/Settings_holo2/Android.bp":                 "packages/apps/Settings/Android.bp",
		"packages-holo2/modules/AdServices/Android.bp":                  "packages/modules/AdServices/Android.bp",
	}
	for laneRel, stockRel := range cases {
		got := stockParallelBP(c, root, filepath.Join(root, filepath.FromSlash(laneRel)))
		want := filepath.Join(root, filepath.FromSlash(stockRel))
		if got != want {
			t.Errorf("stockParallelBP(%q) = %q, want %q", laneRel, got, want)
		}
	}
}

func TestRunRepairInheritedKotlinSourceDrift(t *testing.T) {
	root := t.TempDir()
	stockPath := filepath.Join(root, "frameworks/base/packages/SystemUI/Android.bp")
	lanePath := filepath.Join(root, "frameworks-holo2/base/packages/SystemUI_holo2/Android.bp")
	for _, p := range []string{stockPath, lanePath} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stock := []byte("java_library {\n    name: \"ui\",\n    srcs: [\"src/**/*.java\"],\n}\n")
	lane := []byte("java_library {\n    name: \"ui\",\n    srcs: [\"src/**/*.java\", \"src/**/*.kt\"],\n}\n")
	if err := os.WriteFile(stockPath, stock, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lanePath, lane, 0o644); err != nil {
		t.Fatal(err)
	}

	result := runRepairInheritedKotlinSourceDrift(LaneConfig{Name: "holo2", ParentDirSuffix: "_holo2"}, root)
	if result.Files != 1 || result.Dropped != 1 || result.Repointed != 0 {
		t.Fatalf("result = %+v", result)
	}
	got, err := os.ReadFile(lanePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "**/*.kt") {
		t.Fatalf("empty Kotlin glob survived:\n%s", got)
	}
}

func TestRepairLaneCommandRepairsExistingPermissionHelper(t *testing.T) {
	root := t.TempDir()
	stockPath := filepath.Join(root, "packages/modules/Permission/SafetyCenter/InternalData/Android.bp")
	lanePath := filepath.Join(root, "packages-holo/modules/Permission/SafetyCenter/InternalData/Android.bp")
	if err := os.MkdirAll(filepath.Join(root, "frameworks-holo"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{stockPath, lanePath} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stock := []byte("java_library {\n    name: \"safety-center-internal-data\",\n    srcs: [\"java/**/*.java\"],\n}\n")
	lane := []byte("java_library {\n    name: \"safety-center-internal-data\",\n    srcs: [\"java/**/*.java\", \"java/**/*.kt\"],\n}\n")
	if err := os.WriteFile(stockPath, stock, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lanePath, lane, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := cmdRepairLane([]string{"-name", "holo", "-out", root, "-scope", "packages/modules/Permission/SafetyCenter/InternalData"}); got != 0 {
		t.Fatalf("cmdRepairLane = %d, want 0", got)
	}
	got, err := os.ReadFile(lanePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "**/*.kt") {
		t.Fatalf("repair-lane left empty Kotlin source glob:\n%s", got)
	}
}
