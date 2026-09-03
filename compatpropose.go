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
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	parser "github.com/AbstractsRevenge/sovereign_lane_surgeon/internal/blueprint/parser"
)

// compatpropose.go — `compat-propose`: turn a failed full build into the manifest rows the
// table-driven target-compat operations (5 header exports, 6 proto renames, 8 neverallows) need.
//
// WHY: those operations are correct for what has been observed, but each is a probe plus a table,
// and the table only grows when a build fails and someone reads the log. This command does the
// reading. For each failure class it recognises it looks the missing facts up in the target tree
// — which header library really owns the header, which proto really declares the renamed field,
// which mirrored .te file really carries the violating statement and which platform line forbids
// it — and prints the row in the manifest's own format, ready to append (or appends it with
// -write-to <surgeon source root>; the manifests are embedded, so the binary is rebuilt after).
// The rows keep their probes: a wrong or stale row is a no-op in a tree that does not exhibit
// the condition, which is what makes proposing several candidates safe.

var (
	// clang: fatal error: 'ion/ion.h' file not found
	hdrNotFoundRe = regexp.MustCompile(`fatal error: '([^']+)' file not found`)
	// aprotoc: Option "(android.os.statsd.module)" unknown
	protoOptRe = regexp.MustCompile(`Option "\(([A-Za-z0-9_.]+)\.([A-Za-z0-9_]+)\)" unknown`)
	// checkpolicy: neverallow on line 488 of system/sepolicy/private/dumpstate.te (or line 45678 of policy.conf) violated by allow dumpstate vold:binder { call };
	neverallowRe = regexp.MustCompile(`neverallow on line (\d+) of ([^ ]+) \(or line \d+ of [^)]+\) violated by (allow [^;]+;)`)
	// secilc, CIL form — names BOTH source files and lines, which is better evidence than a tree search:
	//   neverallow check failed at …/plat_sepolicy.cil:27240 from system/sepolicy/private/domain.te:2273
	//     (neverallow base_typeattr_248 base_typeattr_542 (binder (impersonate call set_context_mgr transfer)))
	//       <root>
	//       allow at …/vendor_sepolicy.cil:10260 from device/google/zuma-sepolicy/radio/hal_radioext_default.te:28 from …
	//         (allow hal_radioext_default gril_antenna_tuning_service (binder (call transfer)))
	cilNeverallowRe = regexp.MustCompile(`neverallow check failed at \S+ from (\S+?):(\d+)`)
	cilAllowFromRe  = regexp.MustCompile(`allow at \S+ from (\S+?):(\d+)`)
	// FAILED: … .intermediates/<dir>/<module>/<variant>/…
	intermediatesRe = regexp.MustCompile(`\.intermediates/((?:[A-Za-z0-9_.+-]+/)+)([A-Za-z0-9_.+-]+)/[A-Za-z0-9_.+-]*android[A-Za-z0-9_.+-]*/`)
)

// compatProposal is one manifest row plus where it goes.
type compatProposal struct {
	Manifest string // repo-relative asset manifest
	Row      string // tab-separated, exactly as the manifest wants it
	Why      string
}

// bpModuleIndex maps module name → Android.bp path for the platform directories a mirrored
// device tree links against. Built once per run (a walk of ~5 top-level dirs).
func bpModuleIndex(outRoot string) map[string]string {
	idx := map[string]string{}
	for _, top := range []string{"system", "hardware", "frameworks", "external", "packages/modules"} {
		walkFiles(filepath.Join(outRoot, filepath.FromSlash(top)), isBlueprint, func(p string) {
			b, err := os.ReadFile(p)
			if err != nil {
				return
			}
			file, errs := parser.Parse(p, bytes.NewReader(b))
			if len(errs) > 0 {
				return
			}
			rel, _ := filepath.Rel(outRoot, p)
			for _, def := range file.Defs {
				if m, ok := def.(*parser.Module); ok {
					if _, dup := idx[m.Name()]; !dup {
						idx[m.Name()] = filepath.ToSlash(rel)
					}
				}
			}
		})
	}
	return idx
}

// headerLibOwning finds the cc_library_headers whose export_include_dirs contain header (a
// relative include path such as "ion/ion.h"): module name and its Android.bp.
func headerLibOwning(outRoot, header string, idx map[string]string) (string, string) {
	seen := map[string]bool{}
	var best, bestBp string
	for name, rel := range idx {
		if seen[rel] {
			continue
		}
		seen[rel] = true
		b, err := os.ReadFile(filepath.Join(outRoot, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		file, errs := parser.Parse(rel, bytes.NewReader(b))
		if len(errs) > 0 {
			continue
		}
		for _, def := range file.Defs {
			m, ok := def.(*parser.Module)
			if !ok || m.Type != "cc_library_headers" {
				continue
			}
			dirs := map[string]bool{}
			for _, pr := range m.Properties {
				if pr.Name == "export_include_dirs" {
					collectStrings(pr.Value, dirs)
				}
			}
			for d := range dirs {
				if fileExists(filepath.Join(outRoot, filepath.FromSlash(filepath.ToSlash(filepath.Dir(rel))+"/"+d+"/"+header))) {
					// prefer a vendor-available header lib; first found otherwise
					if best == "" || bpHasProp(m, "vendor_available") {
						best, bestBp = m.Name(), rel
					}
				}
			}
		}
		_ = name
	}
	return best, bestBp
}

func bpHasProp(m *parser.Module, prop string) bool {
	for _, pr := range m.Properties {
		if pr.Name == prop {
			return true
		}
	}
	return false
}

// moduleDeps returns the dep-list entries of module name in the Blueprint under outRoot/dir.
func moduleDeps(outRoot, dir, name string) []string {
	b, err := os.ReadFile(filepath.Join(outRoot, filepath.FromSlash(dir), "Android.bp"))
	if err != nil {
		return nil
	}
	file, errs := parser.Parse(dir, bytes.NewReader(b))
	if len(errs) > 0 {
		return nil
	}
	deps := map[string]bool{}
	for _, def := range file.Defs {
		m, ok := def.(*parser.Module)
		if !ok || m.Name() != name {
			continue
		}
		for _, pr := range m.Properties {
			if depNameProps[pr.Name] {
				collectStrings(pr.Value, deps)
			}
		}
	}
	var out []string
	for d := range deps {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// commonLibs never re-exported a vendor header; they are not proposed as providers.
var commonLibs = map[string]bool{"liblog": true, "libutils": true, "libcutils": true, "libbase": true, "libc++": true, "libbinder": true, "libhidlbase": true, "libbinder_ndk": true}

// proposeHeaderExports: for each "'h' file not found" under a FAILED module, the header library
// that owns h in the target and, as provider candidates, the failing module's non-trivial deps.
func proposeHeaderExports(outRoot string, lines []string, idx map[string]string) []compatProposal {
	var out []compatProposal
	dir, mod := "", ""
	done := map[string]bool{}
	for _, l := range lines {
		if m := intermediatesRe.FindStringSubmatch(l); m != nil && strings.HasPrefix(l, "FAILED") {
			dir, mod = strings.TrimSuffix(m[1], "/"), m[2]
		}
		m := hdrNotFoundRe.FindStringSubmatch(l)
		if m == nil || done[m[1]] {
			continue
		}
		done[m[1]] = true
		header := m[1]
		prefix := header
		if i := strings.Index(header, "/"); i >= 0 {
			prefix = header[:i+1]
		}
		lib, libBp := headerLibOwning(outRoot, header, idx)
		if lib == "" {
			out = append(out, compatProposal{Manifest: "assets/compat/header_exports.MANIFEST", Why: fmt.Sprintf("%s: no cc_library_headers in the target exports a directory holding %s — the header itself is gone, not just its re-export", mod, header)})
			continue
		}
		var cands []string
		for _, d := range moduleDeps(outRoot, dir, mod) {
			if !commonLibs[d] && d != lib {
				if _, known := idx[d]; known {
					cands = append(cands, d)
				}
			}
		}
		if len(cands) == 0 {
			out = append(out, compatProposal{Manifest: "assets/compat/header_exports.MANIFEST", Why: fmt.Sprintf("%s (%s): %s owns %s but the module's Blueprint could not be read for provider candidates", mod, dir, lib, header)})
			continue
		}
		for _, c := range cands {
			out = append(out, compatProposal{
				Manifest: "assets/compat/header_exports.MANIFEST",
				Row:      strings.Join([]string{c, idx[c], strings.TrimSuffix(lib, "_headers"), prefix, lib, libBp, fmt.Sprintf("proposed from %s in %s: '%s' not found; %s owns it. Candidate provider — a row whose provider still exports is a no-op.", mod, dir, header, lib)}, "\t"),
				Why:      fmt.Sprintf("%s in %s includes <%s>; %s (%s) owns it; %s is one of the module's deps", mod, dir, header, lib, libBp, c),
			})
		}
	}
	return out
}

// definingProto finds the .proto under the target's proto directories with `package pkg;` that
// extends google.protobuf options, and the fields it declares.
func definingProto(outRoot, pkg string) (string, []string) {
	var found string
	var fields []string
	for _, top := range []string{"frameworks/proto_logging", "frameworks/base", "packages/modules", "system", "hardware"} {
		if found != "" {
			break
		}
		walkFiles(filepath.Join(outRoot, filepath.FromSlash(top)), func(n string) bool { return strings.HasSuffix(n, ".proto") }, func(p string) {
			if found != "" {
				return
			}
			b, err := os.ReadFile(p)
			if err != nil || !bytes.Contains(b, []byte("package "+pkg+";")) || !bytes.Contains(b, []byte("extend google.protobuf.")) {
				return
			}
			rel, _ := filepath.Rel(outRoot, p)
			found = filepath.ToSlash(rel)
			for _, line := range strings.Split(string(b), "\n") {
				t := strings.TrimSpace(line)
				if i := strings.Index(t, "//"); i >= 0 {
					t = strings.TrimSpace(t[:i])
				}
				f := strings.Fields(strings.TrimSuffix(t, ";"))
				for i := 0; i+2 < len(f); i++ {
					if f[i+1] == "=" {
						if _, err := strconv.Atoi(f[i+2]); err == nil {
							fields = append(fields, f[i])
						}
					}
				}
			}
		})
	}
	return found, fields
}

func proposeProtoRenames(outRoot string, lines []string) []compatProposal {
	var out []compatProposal
	done := map[string]bool{}
	for _, l := range lines {
		m := protoOptRe.FindStringSubmatch(l)
		if m == nil || done[m[1]+"."+m[2]] {
			continue
		}
		done[m[1]+"."+m[2]] = true
		pkg, old := m[1], m[2]
		def, fields := definingProto(outRoot, pkg)
		if def == "" {
			out = append(out, compatProposal{Manifest: "assets/compat/proto_options.MANIFEST", Why: fmt.Sprintf("(%s.%s): no proto with package %s extending google.protobuf options found in the target", pkg, old, pkg)})
			continue
		}
		var news []string
		for _, f := range fields {
			if f != old && strings.HasPrefix(f, old+"_") {
				news = append(news, f)
			}
		}
		if len(news) == 0 {
			out = append(out, compatProposal{Manifest: "assets/compat/proto_options.MANIFEST", Why: fmt.Sprintf("(%s.%s): %s no longer declares %s and no field named %s_* replaces it — the option was removed, not renamed", pkg, old, def, old, old)})
			continue
		}
		for _, n := range news {
			out = append(out, compatProposal{
				Manifest: "assets/compat/proto_options.MANIFEST",
				Row:      strings.Join([]string{def, pkg, old, n, fmt.Sprintf("proposed: aprotoc rejected (%s.%s); %s declares %s", pkg, old, filepath.Base(def), n)}, "\t"),
				Why:      fmt.Sprintf("(%s.%s) unknown; %s declares %s", pkg, old, def, n),
			})
		}
	}
	return out
}

// normalizeAllow reduces "allow a b:c { p q };" to its fields for comparison.
func normalizeAllow(s string) string {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), ";"))
	return strings.Join(strings.Fields(strings.NewReplacer("{", " ", "}", " ", ":", " ").Replace(s)), " ")
}

// mirroredStatement finds, under the mirrored device/hardware trees, the .te line that IS the
// violating allow (same source, target, class and permissions) or — for macro forms such as
// binder_call(a, b) — the line naming both the source and the target.
func mirroredStatement(outRoot, allow string) (string, string) {
	want := normalizeAllow(allow)
	wf := strings.Fields(want)
	if len(wf) < 3 {
		return "", ""
	}
	src, tgt := wf[1], wf[2]
	var file, stmt string
	for _, top := range []string{"device/google", "hardware/google"} {
		if file != "" {
			break
		}
		walkFiles(filepath.Join(outRoot, filepath.FromSlash(top)), func(n string) bool { return strings.HasSuffix(n, ".te") }, func(p string) {
			if file != "" {
				return
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return
			}
			for _, line := range strings.Split(string(b), "\n") {
				t := strings.TrimSpace(line)
				if strings.HasPrefix(t, "#") || t == "" {
					continue
				}
				if normalizeAllow(t) == want || (strings.Contains(t, "(") && strings.Contains(t, src) && strings.Contains(t, tgt)) {
					rel, _ := filepath.Rel(outRoot, p)
					file, stmt = filepath.ToSlash(rel), t
					return
				}
			}
		})
	}
	return file, stmt
}

func fileLine(p string, n int) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if n < 1 || n > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[n-1])
}

func proposeNeverallows(outRoot string, lines []string) []compatProposal {
	var out []compatProposal
	done := map[string]bool{}
	for _, l := range lines {
		m := neverallowRe.FindStringSubmatch(l)
		if m == nil || done[m[3]] {
			continue
		}
		done[m[3]] = true
		n, _ := strconv.Atoi(m[1])
		platform, allow := m[2], m[3]
		head := fileLine(filepath.Join(outRoot, filepath.FromSlash(platform)), n)
		file, stmt := mirroredStatement(outRoot, allow)
		if file == "" || head == "" {
			out = append(out, compatProposal{Manifest: "assets/sepolicy_neverallow/MANIFEST", Why: fmt.Sprintf("%s (forbidden by %s:%d): no mirrored .te line carries it — it may come from a macro expansion elsewhere", allow, platform, n)})
			continue
		}
		out = append(out, compatProposal{
			Manifest: "assets/sepolicy_neverallow/MANIFEST",
			Row:      strings.Join([]string{file, stmt, platform, head, fmt.Sprintf("proposed: secilc reported %s violated by %s (%s:%d)", head, allow, platform, n)}, "\t"),
			Why:      fmt.Sprintf("%s: `%s` violates %s:%d `%s`", file, stmt, platform, n, head),
		})
	}
	return out
}

// proposeNeverallowsCIL reads secilc's CIL-form report: each "neverallow check failed … from
// <platform .te>:<line>" is followed by the "allow at … from <mirrored .te>:<line>" that violates
// it. Both statements are then read from those exact lines — no searching, and the mirrored file
// is named by the compiler itself. Rows are deduplicated per (file, statement).
func proposeNeverallowsCIL(outRoot string, lines []string) []compatProposal {
	var out []compatProposal
	seen := map[string]bool{}
	platFile, platLine := "", 0
	for _, l := range lines {
		if m := cilNeverallowRe.FindStringSubmatch(l); m != nil {
			platFile = m[1]
			platLine, _ = strconv.Atoi(m[2])
			continue
		}
		m := cilAllowFromRe.FindStringSubmatch(l)
		if m == nil || platFile == "" {
			continue
		}
		devFile := m[1]
		devLine, _ := strconv.Atoi(m[2])
		// Only mirrored device/hardware policy is ours to edit; a platform-to-platform
		// violation is the target tree's own business.
		if !strings.HasPrefix(devFile, "device/") && !strings.HasPrefix(devFile, "hardware/") {
			continue
		}
		stmt := fileLine(filepath.Join(outRoot, filepath.FromSlash(devFile)), devLine)
		head := fileLine(filepath.Join(outRoot, filepath.FromSlash(platFile)), platLine)
		if stmt == "" || head == "" {
			out = append(out, compatProposal{Manifest: "assets/sepolicy_neverallow/MANIFEST", Why: fmt.Sprintf("%s:%d violates %s:%d, but one of those lines could not be read", devFile, devLine, platFile, platLine)})
			continue
		}
		key := devFile + "\x00" + stmt
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, compatProposal{
			Manifest: "assets/sepolicy_neverallow/MANIFEST",
			Row:      strings.Join([]string{devFile, stmt, platFile, head, fmt.Sprintf("proposed: secilc reported %s:%d violating %s:%d", devFile, devLine, platFile, platLine)}, "\t"),
			Why:      fmt.Sprintf("%s:%d `%s` violates %s:%d `%s`", devFile, devLine, stmt, platFile, platLine, head),
		})
	}
	return out
}

// proposeCompat runs every detector over the log lines.
func proposeCompat(outRoot string, lines []string) []compatProposal {
	var out []compatProposal
	needIdx := false
	for _, l := range lines {
		if hdrNotFoundRe.MatchString(l) {
			needIdx = true
			break
		}
	}
	var idx map[string]string
	if needIdx {
		idx = bpModuleIndex(outRoot)
	}
	out = append(out, proposeHeaderExports(outRoot, lines, idx)...)
	out = append(out, proposeProtoRenames(outRoot, lines)...)
	out = append(out, proposeNeverallows(outRoot, lines)...)
	out = append(out, proposeNeverallowsCIL(outRoot, lines)...)
	return out
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func cmdCompatPropose(args []string) int {
	fs := flag.NewFlagSet("compat-propose", flag.ExitOnError)
	report := reportFlag(fs)
	out := fs.String("out", "", "the AOSP root the failed build ran in (facts are looked up there)")
	writeTo := fs.String("write-to", "", "append the proposed rows to the manifests under this sovereign-lane-surgeon source root (rebuild after)")
	_ = fs.Parse(args)
	if *report == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "compat-propose: -report and -out are required")
		return 2
	}
	r, err := LoadReport(*report)
	if err != nil {
		fmt.Fprintln(os.Stderr, "compat-propose:", err)
		return 1
	}
	sc, closer, err := r.logScanner()
	if err != nil {
		fmt.Fprintln(os.Stderr, "compat-propose:", err)
		return 1
	}
	defer closer()
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	var lines []string
	for sc.Scan() {
		lines = append(lines, stripANSI(sc.Text()))
	}
	props := proposeCompat(*out, lines)
	if len(props) == 0 {
		fmt.Println("compat-propose: nothing recognised (header-not-found, unknown proto option, neverallow) in this log")
		return 0
	}
	byManifest := map[string][]string{}
	for _, p := range props {
		if p.Row == "" {
			fmt.Printf("[NO ROW] %s\n", p.Why)
			continue
		}
		fmt.Printf("[ROW] %s\n  %s\n  → %s\n", p.Why, p.Row, p.Manifest)
		byManifest[p.Manifest] = append(byManifest[p.Manifest], p.Row)
	}
	if *writeTo == "" {
		return 0
	}
	for man, rows := range byManifest {
		p := filepath.Join(*writeTo, filepath.FromSlash(man))
		f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, "compat-propose:", err)
			return 1
		}
		w := bufio.NewWriter(f)
		for _, row := range rows {
			w.WriteString(row + "\n")
		}
		w.Flush()
		f.Close()
		fmt.Printf("  appended %d row(s) to %s — go build, then re-run create -stock\n", len(rows), p)
	}
	return 0
}

var _ fs.FS = nil // keep io/fs for future in-memory tests
