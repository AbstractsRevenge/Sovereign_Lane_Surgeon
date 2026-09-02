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

func TestNeverallowDropsGatedOnPlatformRule(t *testing.T) {
	drops := embeddedNeverallowDrops()
	if len(drops) < 2 {
		t.Fatalf("manifest: %+v", drops)
	}
	vendor := "# dumpstate\nallow dumpstate vold:binder { call };\nallow dumpstate vold:fd use;\n"
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"system/sepolicy/private/dumpstate.te":                  "neverallow dumpstate {\n  vold\n}:binder call;\n",
		"device/google/gs-common/storage/sepolicy/dumpstate.te": vendor,
	})
	var r compatReport
	dropNeverallowedStatements(root, &r)
	if len(r.SepolicyDrops) != 1 || !strings.Contains(r.SepolicyDrops[0], "allow dumpstate vold:binder { call };") {
		t.Fatalf("report %v", r.SepolicyDrops)
	}
	b, _ := os.ReadFile(filepath.Join(root, "device/google/gs-common/storage/sepolicy/dumpstate.te"))
	if s := string(b); strings.Contains(s, "binder { call }") || !strings.Contains(s, "allow dumpstate vold:fd use;") || !strings.Contains(s, "# dumpstate") {
		t.Fatalf("bad edit:\n%s", s)
	}
	var r2 compatReport
	dropNeverallowedStatements(root, &r2)
	if len(r2.SepolicyDrops) != 0 {
		t.Fatal("not idempotent")
	}
	// A platform without the rule (android-15) leaves the statement alone.
	root2 := t.TempDir()
	writeTree(t, root2, map[string]string{
		"system/sepolicy/private/dumpstate.te":                  "allow dumpstate vold:binder call;\n",
		"device/google/gs-common/storage/sepolicy/dumpstate.te": vendor,
	})
	var r3 compatReport
	dropNeverallowedStatements(root2, &r3)
	if len(r3.SepolicyDrops) != 0 {
		t.Fatalf("no forbidding rule → no drop: %v", r3.SepolicyDrops)
	}
}
