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
	"regexp"
	"strings"
	"testing"
)

// docs_test.go — the documentation is checked against the code the way the code is checked
// against the target tree: README.md and CURRENT_STATE.md drifted twice in two days while the
// tool moved fast, and stale sections hide in a 550-line README. What is pinned here is what a
// reader acts on: the version, every subcommand, the count of compat operations, and the device
// list. A failure names the file and the missing text.

func readDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return string(b)
}

// subcommands is main()'s dispatch table, read from main.go itself so the test cannot drift
// from the code either.
func subcommandsFromMain(t *testing.T) []string {
	src := readDoc(t, "main.go")
	re := regexp.MustCompile(`case "([a-z-]+)"(?:, "[^"]+")*:\n\t\tos\.Exit\(cmd`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		out = append(out, m[1])
	}
	if len(out) < 10 {
		t.Fatalf("found only %d subcommands in main.go: %v", len(out), out)
	}
	return out
}

// prose is README.md plus the two reference files it points at; a subcommand or device must be
// named in at least one of them.
func prose(t *testing.T) string {
	return readDoc(t, "README.md") + readDoc(t, "DESIGN.md") + readDoc(t, "LANES.md")
}

func TestDocsNameEverySubcommand(t *testing.T) {
	readme := prose(t)
	usageText := readDoc(t, "main.go")
	for _, sc := range subcommandsFromMain(t) {
		if !strings.Contains(readme, "`"+sc) && !strings.Contains(readme, sc+" ") && !strings.Contains(readme, sc+"`") {
			t.Errorf("README.md, DESIGN.md and LANES.md never mention subcommand %q", sc)
		}
		if !strings.Contains(usageText, "\n  "+sc) {
			t.Errorf("usage() in main.go does not list subcommand %q", sc)
		}
	}
}

func TestDocsCarryTheVersion(t *testing.T) {
	state := readDoc(t, "CURRENT_STATE.md")
	if !strings.Contains(state, "**Version:** v"+version) {
		t.Errorf("CURRENT_STATE.md header does not say **Version:** v%s", version)
	}
	readme := readDoc(t, "README.md")
	if !strings.Contains(readme, "v"+version) {
		t.Errorf("README.md never mentions v%s", version)
	}
}

// targetCompatOperations is the count applyTargetCompat runs (targetcompat.go); the README's
// numbered list under "Target-release compatibility pass" and CURRENT_STATE's "N operations"
// must agree with it.
func TestDocsCountTheCompatOperations(t *testing.T) {
	readme := readDoc(t, "DESIGN.md") // the operations are documented in the design reference
	numbered := regexp.MustCompile(`(?m)^  (\d+)\. \*`).FindAllStringSubmatch(readme, -1)
	explicit := regexp.MustCompile(`operation (\d+),`).FindAllStringSubmatch(readme, -1)
	max := 0
	for _, m := range append(numbered, explicit...) {
		n := 0
		for _, c := range m[1] {
			n = n*10 + int(c-'0')
		}
		if n > max {
			max = n
		}
	}
	if max != targetCompatOperations {
		t.Errorf("DESIGN.md documents operations up to %d, code runs %d", max, targetCompatOperations)
	}
	state := readDoc(t, "CURRENT_STATE.md")
	want := strings.TrimSpace(strings.Replace(" N operations", "N", itoa(targetCompatOperations), 1))
	if !strings.Contains(state, want) {
		t.Errorf("CURRENT_STATE.md does not say %q", want)
	}
}

func TestDocsListEveryFactoryDevice(t *testing.T) {
	state := readDoc(t, "CURRENT_STATE.md")
	readme := prose(t)
	for _, e := range factoryImageManifest {
		if !strings.Contains(state, "| "+e.Device+" ") {
			t.Errorf("CURRENT_STATE.md coverage table has no row for %s", e.Device)
		}
		if !strings.Contains(readme, e.Device) {
			t.Errorf("README.md, DESIGN.md and LANES.md never mention %s", e.Device)
		}
	}
}

// Every bundle top-level directory has a provenance line, and the provenance names only tags.
func TestBundleProvenanceCoversEveryDirectory(t *testing.T) {
	if !bundleAvailable() {
		t.Skip("nobundle")
	}
	seen := map[string]bool{}
	for _, e := range bundleEntries {
		parts := strings.SplitN(e.Path, "/", 4)
		if len(parts) >= 3 {
			seen[parts[0]+"/"+parts[1]+"/"+parts[2]] = true
		}
	}
	for dir := range seen {
		if tag := bundleSourceTag(dir); !strings.HasPrefix(tag, "android-") {
			t.Errorf("assets/aosp15_device.sources has no tag for %s (got %q)", dir, tag)
		}
	}
}
