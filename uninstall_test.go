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
