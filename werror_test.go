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

func TestDropWerrorOnlyUnderDifferentClang(t *testing.T) {
	src := bundleClang()
	if src == "" {
		t.Fatal("no bundle clang recorded")
	}
	bp := "cc_library {\n    name: \"libpixelstats\",\n    cflags: [\n        \"-Wall\",\n        \"-Werror\",\n    ],\n}\n\ncc_binary {\n    name: \"noflags\",\n    srcs: [\"a.cpp\"],\n}\n\ncc_library_headers {\n    name: \"hdrs\",\n}\n"
	mk := func(clang string) string {
		root := t.TempDir()
		writeTree(t, root, map[string]string{
			soongGlobalCflagsRel:                          "package config\n\nvar (\n\tClangDefaultVersion = \"" + clang + "\"\n\tWarningAllowedProjects = []string{\n\t\t\"device/\",\n\t\t\"vendor/\",\n\t}\n)\n",
			"hardware/google/pixel/pixelstats/Android.bp": bp,
			"device/google/gs-common/x/Android.bp":        bp,
		})
		return root
	}
	same := mk(src)
	var r compatReport
	dropWerrorUnderNewerClang(same, []string{"hardware/google/pixel"}, &r)
	if len(r.WerrorFiles) != 0 {
		t.Fatalf("same clang must keep -Werror: %v", r.WerrorFiles)
	}
	newer := mk("clang-r999999")
	var r2 compatReport
	dropWerrorUnderNewerClang(newer, []string{"hardware/google/pixel", "device/google/gs-common"}, &r2)
	if len(r2.WerrorFiles) != 1 || r2.WerrorFiles[0] != "hardware/google/pixel/pixelstats/Android.bp" {
		t.Fatalf("only the non-allowed dir may change: %v notes %v", r2.WerrorFiles, r2.Notes)
	}
	b, _ := os.ReadFile(filepath.Join(newer, "hardware/google/pixel/pixelstats/Android.bp"))
	s := string(b)
	if strings.Contains(s, "\"-Werror\"") || !strings.Contains(s, "\"-Wall\"") || strings.Count(s, "\"-Wno-error\"") != 2 {
		t.Fatalf("every compiling module gets -Wno-error, headers-only does not:\n%s", s)
	}
	if d, _ := os.ReadFile(filepath.Join(newer, "device/google/gs-common/x/Android.bp")); string(d) != bp {
		t.Fatal("device/ (WarningAllowedProjects) must be untouched")
	}
	var r3 compatReport
	dropWerrorUnderNewerClang(newer, []string{"hardware/google/pixel"}, &r3)
	if len(r3.WerrorFiles) != 0 {
		t.Fatal("not idempotent")
	}
}
