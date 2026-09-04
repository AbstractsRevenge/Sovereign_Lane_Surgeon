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

// TestInversePatchers: append-then-remove returns the original (byte-identical round-trip).
func TestInversePatchers(t *testing.T) {
	// slice: append + remove
	added, _, err := appendStringElem([]byte(isLaneLunchSample), "isLaneLunch", "_zed")
	if err != nil {
		t.Fatal(err)
	}
	back, changed, err := removeStringElem(added, "isLaneLunch", "_zed")
	if err != nil || !changed {
		t.Fatalf("remove: changed=%v err=%v", changed, err)
	}
	if string(back) != isLaneLunchSample {
		t.Errorf("slice round-trip not identical:\n%s", back)
	}
	// remove of an absent elem = no-op
	if _, ch, _ := removeStringElem([]byte(isLaneLunchSample), "isLaneLunch", "_absent"); ch {
		t.Error("removing absent elem should be a no-op")
	}
}

// TestRemoveDeclByName: removes a named func + its doc comment; re-parses clean; no-op if absent.
func TestRemoveDeclByName(t *testing.T) {
	src := `package p

// keepMe stays.
func keepMe() {}

// dropMe is removed.
func dropMe() { println("x") }

func alsoKeep() {}
`
	out, changed, err := removeDeclByName([]byte(src), "dropMe")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	s := string(out)
	if strings.Contains(s, "dropMe") || strings.Contains(s, "is removed") {
		t.Errorf("dropMe (or its doc) not removed:\n%s", s)
	}
	if !strings.Contains(s, "func keepMe()") || !strings.Contains(s, "func alsoKeep()") {
		t.Error("removed too much")
	}
	if _, ch, _ := removeDeclByName(out, "dropMe"); ch {
		t.Error("removing absent decl should be a no-op")
	}
}

// --- v0.3.0: uninstall -target splits trees from patches ---

// A full uninstall discards the lane CLONE, which for a real lane is multi-GB and minutes to
// rebuild. -target patches reverts the registration while keeping the trees, so a lane can be
// re-seeded (renamed, re-registered) without re-cloning; -target lanes is the converse.
func TestUninstallTargetSplit(t *testing.T) {
	seed := func(t *testing.T) string {
		root := t.TempDir()
		for _, d := range []string{"frameworks-zed/base", "packages-zed/apps", ".zed"} {
			if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		p := filepath.Join(root, "device/generic/goldfish/64bitonly/product")
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "sdk_phone64_zed.mk"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return root
	}
	treesGone := func(root string) bool {
		for _, d := range []string{"frameworks-zed", "packages-zed", ".zed",
			"device/generic/goldfish/64bitonly/product/sdk_phone64_zed.mk"} {
			if _, err := os.Stat(filepath.Join(root, d)); err == nil {
				return false
			}
		}
		return true
	}
	c := deriveLane("zed", true, nil, false, false, "")

	root := seed(t)
	uninstallLane(c, root, "patches")
	if treesGone(root) {
		t.Error("-target patches must KEEP the lane trees")
	}

	root = seed(t)
	uninstallLane(c, root, "lanes")
	if !treesGone(root) {
		t.Error("-target lanes must remove the lane trees")
	}

	root = seed(t)
	uninstallLane(c, root, "all")
	if !treesGone(root) {
		t.Error("-target all must remove the lane trees")
	}
}
