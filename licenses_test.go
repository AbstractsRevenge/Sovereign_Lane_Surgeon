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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// licenses_test.go — every file authored HERE carries the author's copyright line dated 2026, the
// project line, and the Apache 2.0 block; every file redistributed verbatim keeps the copyright it
// came with (and must NOT carry the author's).
//
// Enforced rather than swept, for the same reason docs_test.go exists: a one-time pass is correct
// until the next file is added. The canonical forms live in androidbp_apache2_header.md and the
// upstream content is listed in NOTICE.

// upstreamPrefixes are redistributed verbatim: relicensing or re-dating them would misstate their
// provenance. Each is recorded in NOTICE.
var upstreamPrefixes = []string{
	"assets/aosp15_device/",     // AOSP device + hardware trees (r36, tegu from r31)
	"assets/kernel_headers/",    // vendor kernel UAPI headers from the same tags (MANIFEST is ours)
	"assets/overlays/hardware/", // upstream Soong-conversion sources, original years 2012-2024
	"internal/blueprint/",       // vendored Blueprint parser, Google 2014
}

// binaryAssets cannot carry a comment at all. They are covered by the repository LICENSE, and
// listed here rather than skipped silently so that adding one is a deliberate act.
func isBinaryAsset(rel string) bool {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".pdf", ".zip", ".gz", ".img":
		return true
	}
	return false
}

// noHeaderNeeded cannot carry a comment, or has no comment convention worth imposing.
var noHeaderNeeded = map[string]bool{
	"go.mod":  true, // module files take no license header by convention
	"go.sum":  true,
	"LICENSE": true, // is the license
	"NOTICE":  true, // carries its own attribution text
}

func trackedFiles(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("git", "ls-files").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	var files []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	if len(files) < 50 {
		t.Fatalf("git ls-files returned only %d paths", len(files))
	}
	return files
}

func authoredHere(rel string) bool {
	if noHeaderNeeded[rel] || isBinaryAsset(rel) {
		return false
	}
	for _, p := range upstreamPrefixes {
		if strings.HasPrefix(rel, p) {
			// The MANIFEST describing an upstream directory is ours.
			return filepath.Base(rel) == "MANIFEST"
		}
	}
	return true
}

func TestEveryAuthoredFileCarriesTheApacheHeader(t *testing.T) {
	const (
		aosp    = "Copyright 2026 Terrance Leverette (AbstractsRevenge)"
		project = "Sovereign Lane Surgeon: https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon"
		apache  = "Licensed under the Apache License, Version 2.0"
	)
	checked := 0
	for _, rel := range trackedFiles(t) {
		if !authoredHere(rel) {
			continue
		}
		b, err := os.ReadFile(rel)
		if err != nil {
			if os.IsNotExist(err) {
				continue // tracked but deleted in the worktree: a staging state, not a licence problem
			}
			t.Errorf("%s: %v", rel, err)
			continue
		}
		// The header is at the top: a generous window covers a build tag or a doc title above it.
		head := string(b)
		if len(head) > 4096 {
			head = head[:4096]
		}
		checked++
		for _, want := range []string{aosp, project, apache} {
			if !strings.Contains(head, want) {
				t.Errorf("%s: missing %q in its first 4KB", rel, want)
			}
		}
	}
	if checked < 90 {
		t.Fatalf("only %d authored files checked — the exclusion rules are too broad", checked)
	}
	t.Logf("%d authored files carry the header", checked)
}

// Verbatim upstream files must NOT have been re-dated to 2026: that would misstate provenance.
func TestUpstreamFilesKeepTheirOwnCopyright(t *testing.T) {
	const project = "Copyright 2026 Terrance Leverette"
	checked := 0
	for _, rel := range trackedFiles(t) {
		if authoredHere(rel) || noHeaderNeeded[rel] || isBinaryAsset(rel) {
			continue
		}
		b, err := os.ReadFile(rel)
		if err != nil {
			continue
		}
		checked++
		if strings.Contains(string(b), project) {
			t.Errorf("%s is redistributed verbatim but carries this project's copyright", rel)
		}
	}
	if checked == 0 {
		t.Fatal("no upstream files were checked")
	}
	t.Logf("%d redistributed files keep their own copyright", checked)
}

// The root license files must exist and be the real Apache 2.0 text.
func TestLicenseAndNoticeExist(t *testing.T) {
	b, err := os.ReadFile("LICENSE")
	if err != nil {
		t.Fatalf("LICENSE: %v", err)
	}
	for _, want := range []string{"Apache License", "Version 2.0, January 2004", "APPENDIX: How to apply the Apache License"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("LICENSE does not look like the Apache 2.0 text: missing %q", want)
		}
	}
	n, err := os.ReadFile("NOTICE")
	if err != nil {
		t.Fatalf("NOTICE: %v", err)
	}
	for _, p := range upstreamPrefixes {
		if !strings.Contains(string(n), strings.TrimSuffix(p, "/")) {
			t.Errorf("NOTICE does not account for redistributed content under %s", p)
		}
	}
}
