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
	_ "embed"
	"os"
	"path/filepath"
	"strings"
)

// sepolicyneverallow.go — target-compat operation 8: vendor policy statements the target
// platform's neverallows forbid (assets/sepolicy_neverallow/MANIFEST).
//
// WHY (observed 2026-09-02, cheetah `m droid superimage`, Build Capture run 144400Z): secilc's
// final neverallow check named two android-15 vendor statements android-17's platform policy
// forbids: gs-common's `allow dumpstate vold:binder { call };` (17 adds `neverallow dumpstate
// { vold }:binder call`) and `binder_call(vendor_pcs_app, hal_pixel_remote_camera_service);`
// (a binder call to a service NAME, which 17 rejects with `neverallow * { -domain }:binder *`).
// A neverallow cannot be computed here without the policy compiler, so the evidence is recorded
// as data: each manifest line names the mirrored file, the exact statement, the platform file and
// a line of the forbidding rule. The statement is dropped only while the platform still carries
// that rule. `audit` classifies a fresh "neverallow check failed" as sepolicy-neverallow-violation
// with the same recipe.

//go:embed assets/sepolicy_neverallow/MANIFEST
var embeddedNeverallowManifest string

type neverallowDrop struct {
	File, Statement, PlatformFile, PlatformLine, Note string
}

func embeddedNeverallowDrops() []neverallowDrop {
	var out []neverallowDrop
	for _, line := range strings.Split(embeddedNeverallowManifest, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 4 {
			continue
		}
		d := neverallowDrop{File: f[0], Statement: strings.TrimSpace(f[1]), PlatformFile: f[2], PlatformLine: strings.TrimSpace(f[3])}
		if len(f) > 4 {
			d.Note = f[4]
		}
		out = append(out, d)
	}
	return out
}

// fileHasLine reports whether any trimmed line of the file equals want.
func fileHasLine(p, want string) bool {
	b, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == want {
			return true
		}
	}
	return false
}

// dropExactLine removes every line whose trimmed text equals stmt.
func dropExactLine(content []byte, stmt string) ([]byte, int) {
	lines := strings.Split(string(content), "\n")
	kept := lines[:0]
	n := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == stmt {
			n++
			continue
		}
		kept = append(kept, l)
	}
	if n == 0 {
		return content, 0
	}
	return []byte(strings.Join(kept, "\n")), n
}

func dropNeverallowedStatements(outRoot string, r *compatReport) {
	for _, d := range embeddedNeverallowDrops() {
		if !fileHasLine(filepath.Join(outRoot, filepath.FromSlash(d.PlatformFile)), d.PlatformLine) {
			continue // the target does not carry the forbidding rule
		}
		p := filepath.Join(outRoot, filepath.FromSlash(d.File))
		b, err := os.ReadFile(p)
		if err != nil {
			continue // that subtree was not revived
		}
		out, n := dropExactLine(b, d.Statement)
		if n == 0 || os.WriteFile(p, out, 0o644) != nil {
			continue
		}
		r.SepolicyDrops = append(r.SepolicyDrops, d.File+": dropped `"+d.Statement+"` (forbidden by the target's "+d.PlatformFile+": "+d.PlatformLine+")")
	}
}
