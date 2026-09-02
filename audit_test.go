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

import "testing"

// A Build Capture run is green for every success status Build Capture itself emits, including
// completed_bootstrap — the `m nothing` gate this toolkit's revival loop is built on. A failed
// exit is never green regardless of status, and an unknown status is never green.
func TestReportGreenMatchesBuildCaptureVocabulary(t *testing.T) {
	cases := []struct {
		status  string
		success bool
		want    bool
	}{
		{"full_completed", true, true},
		{"completed_incremental", true, true},
		{"completed_noop", true, true},
		{"completed_bootstrap", true, true},
		{"completed_bootstrap", false, false},
		{"failed_soong", false, false},
		{"failed_soong", true, false},
		{"", true, false},
	}
	for _, c := range cases {
		r := &Report{Success: c.success, DirStatus: c.status}
		if got := r.green(); got != c.want {
			t.Errorf("green(status=%q success=%v) = %v, want %v", c.status, c.success, got, c.want)
		}
	}
}
