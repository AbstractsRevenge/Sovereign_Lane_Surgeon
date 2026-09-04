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

package bpflags

import (
	"strings"
	"testing"
)

func TestDropNestedAndIdempotent(t *testing.T) {
	src := `cc_binary {
    name: "dump_gsa",
    cflags: [
        "-Wall",
        "-pedantic",
    ],
    arch: {
        arm64: {
            cppflags: ["-pedantic", "-O2"],
        },
    },
}
`
	drop := map[string]bool{"-pedantic": true}
	out, ch, err := Drop([]byte(src), drop)
	if err != nil || !ch {
		t.Fatalf("changed=%v err=%v", ch, err)
	}
	s := string(out)
	if strings.Contains(s, "-pedantic") {
		t.Fatalf("flag survived:\n%s", s)
	}
	if !strings.Contains(s, `"-Wall"`) || !strings.Contains(s, `"-O2"`) {
		t.Fatalf("neighbours lost:\n%s", s)
	}
	again, ch2, err := Drop(out, drop)
	if err != nil || ch2 || string(again) != s {
		t.Fatalf("not idempotent: changed=%v err=%v", ch2, err)
	}
}
