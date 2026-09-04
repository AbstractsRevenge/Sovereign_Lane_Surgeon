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
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Report is the subset of a build-capture terminal_log.json the tool reads.
type Report struct {
	SchemaVersion string  `json:"schema_version"`
	Lane          string  `json:"lane"`
	LunchTarget   string  `json:"lunch_target"`
	ExitCode      int     `json:"exit_code"`
	Success       bool    `json:"success"`
	DirStatus     string  `json:"dir_status"`
	DurationSec   float64 `json:"duration_sec"`
	RawLogPath    string  `json:"raw_log_path"`
	TerminalLog   string  `json:"terminal_log"`
	BuildOutDir   string  `json:"build_out_dir"`
}

// green reports whether a Build Capture run is a pass. Build Capture's own vocabulary
// (buildlog/dirnaming.go) has FOUR success statuses: full_completed (real work compiled),
// completed_incremental (mostly cached), completed_noop (ninja had nothing to do) and
// completed_bootstrap (`m nothing`: analysis + kati only, no compile). The last one is the
// graph-coherence gate this whole toolkit's create -stock -> `m nothing` -> audit loop runs
// on, so treating it as NOT GREEN made `verify`/`audit` contradict the run-dir name on every
// successful gate (observed 2026-09-02: aosp_panther-cp2a-eng, 0 errors, VERDICT: NOT GREEN).
func (r *Report) green() bool {
	if !r.Success {
		return false
	}
	switch r.DirStatus {
	case "full_completed", "completed_incremental", "completed_noop", "completed_bootstrap":
		return true
	}
	return false
}

// LoadReport accepts a run-dir or a terminal_log.json path.
func LoadReport(path string) (*Report, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	jsonPath := path
	if fi.IsDir() {
		jsonPath = filepath.Join(path, "terminal_log.json")
	}
	b, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("read report: %w", err)
	}
	var r Report
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("parse report %s: %w", jsonPath, err)
	}
	return &r, nil
}

// Finding is one classified failure.
type Finding struct {
	Module string
	Class  *BlockerClass // nil = unclassified
}

var (
	failedRe = regexp.MustCompile(`^FAILED: `)
	progRe   = regexp.MustCompile(`^\[ *\d+% `)
	// module name = the component just before the variant dir in an .intermediates path.
	modRe   = regexp.MustCompile(`\.intermediates/(?:.*/)?([A-Za-z0-9_.+-]+)/[a-z0-9_]+(?:_[a-z0-9]+)?/(?:javac|kotlinc|turbine|kapt|clang\+?\+?|link|d8|r8|aidl|proto|gen[a-z]*|aapt2|fix)`)
	checkRe = regexp.MustCompile(`([A-Za-z0-9_./-]+\.(?:check|txt))\b`)
)

func moduleFromFailed(line string) string {
	if m := modRe.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	if m := checkRe.FindStringSubmatch(strings.TrimPrefix(line, "FAILED: ")); m != nil {
		return filepath.Base(m[1])
	}
	// fall back to the first output token
	f := strings.Fields(strings.TrimPrefix(line, "FAILED: "))
	if len(f) > 0 {
		return filepath.Base(f[0])
	}
	return "?"
}

// Classify scans the run's raw log, extracts each FAILED module, and matches the
// taxonomy against a window of lines following its FAILED marker.
func Classify(r *Report) []Finding {
	if r.green() {
		return nil
	}
	sc, closeFn, err := r.logScanner()
	if err != nil {
		return []Finding{{Module: "(log unavailable: " + err.Error() + ")"}}
	}
	defer closeFn()

	const window = 60
	type pending struct {
		mod   string
		lines []string
		left  int
	}
	var findings []Finding
	seen := map[string]bool{}
	var open []*pending

	flush := func(p *pending) {
		key := p.mod
		if seen[key] {
			return
		}
		seen[key] = true
		cls := classForText(strings.Join(p.lines, "\n"))
		findings = append(findings, Finding{Module: p.mod, Class: cls})
	}

	for sc.Scan() {
		line := sc.Text()
		// advance any open error windows
		next := open[:0]
		for _, p := range open {
			if progRe.MatchString(line) || failedRe.MatchString(line) {
				// window ends at the next progress/FAILED line
				flush(p)
				continue
			}
			p.lines = append(p.lines, line)
			p.left--
			if p.left <= 0 {
				flush(p)
				continue
			}
			next = append(next, p)
		}
		open = next
		if failedRe.MatchString(line) {
			open = append(open, &pending{mod: moduleFromFailed(line), lines: []string{line}, left: window})
		}
	}
	for _, p := range open {
		flush(p)
	}
	return findings
}

func (r *Report) logScanner() (*bufio.Scanner, func(), error) {
	if r.RawLogPath != "" {
		if f, err := os.Open(r.RawLogPath); err == nil {
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 0, 1<<20), 1<<24) // long build lines
			return sc, func() { f.Close() }, nil
		}
	}
	if r.TerminalLog != "" {
		sc := bufio.NewScanner(strings.NewReader(r.TerminalLog))
		sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
		return sc, func() {}, nil
	}
	return nil, nil, fmt.Errorf("no raw_log_path and no embedded terminal_log")
}

func cmdAudit(args []string) int {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	report := reportFlag(fs)
	_ = fs.Parse(args)
	if *report == "" {
		fmt.Fprintln(os.Stderr, "audit: -report is required")
		return 2
	}
	r, err := LoadReport(*report)
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit:", err)
		return 1
	}
	fmt.Printf("# lane=%s lunch=%s dir_status=%s\n", r.Lane, r.LunchTarget, r.DirStatus)
	if r.green() {
		fmt.Println("# GREEN — no failures to classify.")
		return 0
	}
	findings := Classify(r)
	if len(findings) == 0 {
		fmt.Println("# NOT GREEN but no FAILED modules found in the log (check the log manually).")
		return 1
	}
	fmt.Println("module\tclass\tlayer\tauto\tlane_defect\trecipe")
	nLane := 0
	for _, f := range findings {
		if f.Class == nil {
			fmt.Printf("%s\tUNCLASSIFIED\t-\t-\t-\t(no taxonomy signature matched — inspect the log / extend the taxonomy)\n", f.Module)
			nLane++
			continue
		}
		c := f.Class
		if c.LaneDefect {
			nLane++
		}
		fmt.Printf("%s\t%s\t%s\t%v\t%v\t%s\n", f.Module, c.Name, c.Layer, c.Auto, c.LaneDefect, c.Recipe)
	}
	fmt.Printf("# %d finding(s); %d are lane defects (the rest are infra/environmental — retry, don't fix).\n", len(findings), nLane)
	return 0
}
