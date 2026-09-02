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
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// reconcile.go — cross-tag reconciliations (assets/reconcile/MANIFEST).
//
// WHY (observed 2026-09-02): the bundle is one tree from two AOSP tags — every family from
// android-15.0.0_r36 except tegu (Pixel 9a, android-15.0.0_r31 only). A family from the other tag
// can still include a makefile the shared tag removed: device-tegu.mk includes
// hardware/google/pixel/vibrator/cs40l26/device.mk (kati: "No such file or directory", run
// 130318Z). r36 dropped that whole directory, moved its flag modules to gs-common under the same
// names, and its own devices get the HAL as a vendor blob — which tegu's factory image ships too.
// Supplying r31's directory instead (tried, run 130649Z) duplicates the aconfig package and Soong
// panics. The faithful move is the one r36 made for every family: drop the include. Each such
// line is listed explicitly with its evidence and dropped only when the path is really absent.

//go:embed assets/reconcile/MANIFEST
var embeddedReconcileManifest string

type reconcileEntry struct {
	Makefile string // repo-relative, forward-slashed
	Include  string // the included path
	Note     string
}

func embeddedReconcileEntries() []reconcileEntry {
	var out []reconcileEntry
	for _, line := range strings.Split(embeddedReconcileManifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		e := reconcileEntry{Makefile: f[0], Include: f[1]}
		if len(f) > 2 {
			e.Note = strings.Join(f[2:], " ")
		}
		out = append(out, e)
	}
	return out
}

// dropIncludeLine removes every line that is exactly `include <inc>` or `-include <inc>` (after
// trimming). Line scan, no regex (HARD RULE 3).
func dropIncludeLine(content []byte, inc string) ([]byte, bool) {
	lines := strings.Split(string(content), "\n")
	kept := lines[:0]
	changed := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "include "+inc || t == "-include "+inc {
			changed = true
			continue
		}
		kept = append(kept, l)
	}
	if !changed {
		return content, false
	}
	return []byte(strings.Join(kept, "\n")), true
}

// applyReconciliations drops each listed include whose target is absent from outRoot. Returns
// "makefile: dropped include <path>" lines for the report.
func applyReconciliations(outRoot string) ([]string, error) {
	var report []string
	for _, e := range embeddedReconcileEntries() {
		if _, err := os.Stat(filepath.Join(outRoot, filepath.FromSlash(e.Include))); err == nil {
			continue // the target has it — nothing to reconcile
		}
		mk := filepath.Join(outRoot, filepath.FromSlash(e.Makefile))
		b, err := os.ReadFile(mk)
		if err != nil {
			continue // that family was not revived
		}
		out, ch := dropIncludeLine(b, e.Include)
		if !ch {
			continue
		}
		if err := os.WriteFile(mk, out, 0o644); err != nil {
			return report, fmt.Errorf("reconcile %s: %w", e.Makefile, err)
		}
		report = append(report, e.Makefile+": dropped include "+e.Include)
	}
	return report, nil
}
