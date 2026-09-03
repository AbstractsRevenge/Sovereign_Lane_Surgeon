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
	"bytes"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	parser "github.com/AbstractsRevenge/sovereign_lane_surgeon/internal/blueprint/parser"
	"github.com/AbstractsRevenge/sovereign_lane_surgeon/internal/bpflags"
)

// targetcompat.go — the target-release compatibility pass `create -stock` runs over every subtree
// it mirrored. A device tree cut from one AOSP release carries build-file idioms a newer release
// rejects; each operation here answers ONE such rejection observed on android-17.0.0_r1 with the
// AOSP 15 (r36) Pixel 7 trees (2026-09-02), and each is gated on a probe of the TARGET tree rather
// than on a release number, so a tree that does not reject the idiom is left exactly as mirrored:
//
//  1. Illegal cflags — Soong: `cflags: Illegal flag "-pedantic"` (device/google/gs-common/gsa,
//     hardware/google/pixel/recovery). The list is READ from the target's
//     build/soong/cc/config/global.go (`IllegalFlags = []string{...}`, go/ast) and every entry is
//     removed from cflags/cppflags/conlyflags in the mirrored Blueprints (internal/bpflags).
//  2. System properties — fsgen: "Device makefile has PRODUCT_SYSTEM_PROPERTIES or
//     PRODUCT_SYSTEM_DEFAULT_PROPERTIES that add properties to the 'system' partition, which is
//     against generic_system.mk's artifact path requirement" (build/soong/fsgen/
//     artifact_path_requirements.go, fires regardless of `relaxed`). The message itself names the
//     fix: PRODUCT_PRODUCT_PROPERTIES. Assignment lines are rewritten in the mirrored product
//     makefiles (factory_*.mk excluded — not in any aosp_ product chain).
//  3. AIDL version conflicts — Soong: `module "libpixelhealth" … depends on multiple versions of
//     the same aidl_interface: android.hardware.health-V4-ndk-source, android.hardware.health-V5-
//     ndk-source`. A mirrored module pins X-V<n>-<lang> AND links a sibling library defined in
//     X's own Android.bp (the translate library) that itself links X-V<m>-<lang>, m≠n. Measured
//     over every pin the bundle carries against android-17: exactly one module matches (health),
//     while a naive "re-pin to the target's latest" would have touched 16 pins that build fine
//     as they are (hwc3 at composer3-V4 among them). The pin is renamed to the sibling's version
//     with renameAndRepointBp (AST, def + every dep ref in that file).
//  5. Lost transitive header exports — compile: "'ion/ion.h' file not found" in graphics/common/
//     libion (headerexports.go): android-17's libdmabufheap no longer re-exports libion's headers,
//     so a module including them through it gets header_libs: ["libion_headers"] — only when the
//     target's provider Blueprint really lacks the export and the header library exists.
//  6. Renamed proto options — aprotoc: `Option "(android.os.statsd.module)" unknown` in
//     pixelstats (protooptions.go): the target's atom_field_options.proto moved `module` to
//     `module_name`; the exact option token is renamed in the mirrored .proto files, only when the
//     target's defining proto really declares the new name and not the old.
//  7. Platform-declared SELinux types — checkpolicy: `Duplicate declaration of type` for
//     vendor_chre_hal_prop (sepolicydecls.go): android-17's system/sepolicy took the declaration
//     over; the mirrored declaration line is dropped when the platform declares the same name.
//  8. Neverallowed vendor statements — secilc: `neverallow check failed` (sepolicyneverallow.go):
//     statements recorded with the platform rule that forbids them, dropped while the target
//     carries that rule.
//  9. -Werror under a newer toolchain — compile: `unused variable … [-Werror,-Wunused-variable]`
//     in code byte-identical to Google's current tree (werror.go): the exact "-Werror" entry is
//     dropped from mirrored Blueprints when the target's clang differs from the bundle tag's.
// 10. HALs floating on a newer AIDL version than they declare — compile: power-libperfmgr's
//     static_assert on SessionMode (aidlfloat.go): a module floating on X-ndk_shared/_static whose
//     vintf fragment declares X at V<N> is pinned to X-V<N>-ndk when the target resolves higher.
//  4. Denylisted Android.mk — Soong: "Found blocked Android.mk file: hardware/google/graphics/
//     common/Android.mk" (build/soong/ui/build/androidmk_denylist.go blocks device/google/ and
//     hardware/google/ wholesale). The blocked makefiles are replaced by Google's own Soong
//     conversion carried as an embedded overlay (overlays.go, assets/overlays/MANIFEST); a blocked
//     makefile with no overlay is reported, not guessed at.
//
// Everything is idempotent: a second run over an already-treated tree changes nothing.

// compatReport is what applyTargetCompat did, for the caller's summary.
type compatReport struct {
	IllegalFlags  []string
	FlagFiles     []string // repo-relative Blueprints edited
	PropFiles     []string // repo-relative makefiles edited
	AidlRepins    []string // "rel: old → new"
	Overlays      []string // "subtree ← source@commit"
	MkRemoved     []string // blocked Android.mk removed under an overlaid subtree
	OverlayFiles  []string // overlay files written
	BlockedMk     []string // blocked Android.mk with NO overlay — a real blocker
	HeaderLibs    []string // "rel: module += header_libs X (why)"
	ProtoRenames  []string // "rel: (old) → (new) ×n (why)"
	SepolicyDrops []string // "rel: dropped declaration of X (why)"
	WerrorFiles   []string // Blueprints whose "-Werror" was dropped under a newer clang
	AidlPins      []string // "rel: module: floating default → pinned lib (why)"
	Notes         []string
}

// walkFiles calls fn for every regular file under root (skipping .git/.repo) whose name satisfies
// match. A missing root is not an error — the caller may name a subtree that was never mirrored.
func walkFiles(root string, match func(name string) bool, fn func(path string)) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".repo" {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && match(d.Name()) {
			fn(p)
		}
		return nil
	})
}

func isBlueprint(name string) bool { return name == "Android.bp" }

// ─── 1. illegal cflags ───

const soongGlobalCflagsRel = "build/soong/cc/config/global.go"

// soongIllegalFlags returns the target tree's IllegalFlags list, parsed out of Soong's own source
// with go/ast — the same list Soong enforces, never a hand-maintained copy.
func soongIllegalFlags(outRoot string) ([]string, error) {
	p := filepath.Join(outRoot, filepath.FromSlash(soongGlobalCflagsRel))
	f, err := goparser.ParseFile(token.NewFileSet(), p, nil, 0)
	if err != nil {
		return nil, err
	}
	var flags []string
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if name.Name != "IllegalFlags" || i >= len(vs.Values) {
				continue
			}
			cl, ok := vs.Values[i].(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, el := range cl.Elts {
				if lit, ok := el.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, uerr := strconv.Unquote(lit.Value); uerr == nil && s != "" {
						flags = append(flags, s)
					}
				}
			}
		}
		return true
	})
	if len(flags) == 0 {
		return nil, fmt.Errorf("%s: no IllegalFlags list", soongGlobalCflagsRel)
	}
	return flags, nil
}

func dropIllegalCflags(outRoot string, roots []string, r *compatReport) {
	flags, err := soongIllegalFlags(outRoot)
	if err != nil {
		r.Notes = append(r.Notes, "illegal cflags: skipped — "+err.Error())
		return
	}
	r.IllegalFlags = flags
	drop := map[string]bool{}
	for _, f := range flags {
		drop[f] = true
	}
	for _, root := range roots {
		walkFiles(filepath.Join(outRoot, root), isBlueprint, func(p string) {
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return
			}
			out, ch, derr := bpflags.Drop(b, drop)
			if derr != nil || !ch {
				return
			}
			if os.WriteFile(p, out, 0o644) == nil {
				rel, _ := filepath.Rel(outRoot, p)
				r.FlagFiles = append(r.FlagFiles, filepath.ToSlash(rel))
			}
		})
	}
}

// ─── 2. system properties off the system partition ───

const (
	fsgenArtifactReqRel = "build/soong/fsgen/artifact_path_requirements.go"
	systemPropsVar      = "PRODUCT_SYSTEM_PROPERTIES"
	productPropsVar     = "PRODUCT_PRODUCT_PROPERTIES"
)

// fsgenRejectsSystemProps probes whether the target's fsgen enforces the system-partition
// property check at all (android-15 has no such file; android-17 does).
func fsgenRejectsSystemProps(outRoot string) bool {
	b, err := os.ReadFile(filepath.Join(outRoot, filepath.FromSlash(fsgenArtifactReqRel)))
	return err == nil && bytes.Contains(b, []byte(systemPropsVar))
}

// moveSystemPropsToProduct rewrites every PRODUCT_SYSTEM_PROPERTIES assignment line (`+=`, `:=`,
// `=`) to PRODUCT_PRODUCT_PROPERTIES, keeping indentation, operator and the value (including a
// trailing continuation). References inside a value (`$(PRODUCT_SYSTEM_PROPERTIES)`) and longer
// variable names (PRODUCT_SYSTEM_PROPERTIES_FOO) are not assignments and are left alone. Line
// scan, no regex (HARD RULE 3).
func moveSystemPropsToProduct(content []byte) ([]byte, bool) {
	lines := strings.Split(string(content), "\n")
	changed := false
	for i, line := range lines {
		t := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(t, systemPropsVar) {
			continue
		}
		rest := t[len(systemPropsVar):]
		if rest == "" {
			continue
		}
		switch rest[0] {
		case ' ', '\t', '+', ':', '=':
		default:
			continue // a longer variable name
		}
		lines[i] = line[:len(line)-len(t)] + productPropsVar + rest
		changed = true
	}
	if !changed {
		return content, false
	}
	return []byte(strings.Join(lines, "\n")), true
}

func isProductMakefile(name string) bool {
	return strings.HasSuffix(name, ".mk") && !strings.HasPrefix(name, "factory")
}

func moveSystemProps(outRoot string, roots []string, r *compatReport) {
	if !fsgenRejectsSystemProps(outRoot) {
		r.Notes = append(r.Notes, "system properties: target fsgen has no system-partition property check — left as mirrored")
		return
	}
	for _, root := range roots {
		walkFiles(filepath.Join(outRoot, root), isProductMakefile, func(p string) {
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return
			}
			out, ch := moveSystemPropsToProduct(b)
			if !ch {
				return
			}
			if os.WriteFile(p, out, 0o644) == nil {
				rel, _ := filepath.Rel(outRoot, p)
				r.PropFiles = append(r.PropFiles, filepath.ToSlash(rel))
			}
		})
	}
}

// ─── 3. AIDL version conflicts ───

// aidlInterfaceRoots are the trees that define the aidl_interface modules device code pins.
var aidlInterfaceRoots = []string{
	"hardware/interfaces", "frameworks/hardware/interfaces", "system/hardware/interfaces", "hardware/google/interfaces",
}

type aidlPin struct {
	Iface string
	Ver   int
	Lang  string
}

func (p aidlPin) String() string { return p.Iface + "-V" + strconv.Itoa(p.Ver) + "-" + p.Lang }

// parseAidlPin recognizes "<iface>-V<n>-<ndk|cpp|java|rust>".
func parseAidlPin(s string) (aidlPin, bool) {
	i := strings.LastIndex(s, "-")
	if i <= 0 {
		return aidlPin{}, false
	}
	lang := s[i+1:]
	switch lang {
	case "ndk", "cpp", "java", "rust":
	default:
		return aidlPin{}, false
	}
	head := s[:i]
	j := strings.LastIndex(head, "-V")
	if j <= 0 || j+2 >= len(head) {
		return aidlPin{}, false
	}
	n, err := strconv.Atoi(head[j+2:])
	if err != nil || n <= 0 {
		return aidlPin{}, false
	}
	return aidlPin{Iface: head[:j], Ver: n, Lang: lang}, true
}

// collectStrings gathers every string leaf of an expression (List, `+` concatenations, select
// branches) — the same shapes repointExprRefs walks.
func collectStrings(e parser.Expression, out map[string]bool) {
	switch v := e.(type) {
	case *parser.String:
		out[v.Value] = true
	case *parser.List:
		for _, it := range v.Values {
			collectStrings(it, out)
		}
	case *parser.Operator:
		collectStrings(v.Args[0], out)
		collectStrings(v.Args[1], out)
	case *parser.Select:
		for _, c := range v.Cases {
			if c != nil && c.Value != nil {
				collectStrings(c.Value, out)
			}
		}
		if v.Append != nil {
			collectStrings(v.Append, out)
		}
	}
}

// bpModuleDeps maps each named module in a parsed Blueprint to the bare names it lists in its
// dependency properties (depNameProps, renamepass.go).
func bpModuleDeps(file *parser.File) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, def := range file.Defs {
		m, ok := def.(*parser.Module)
		if !ok || m.Name() == "" {
			continue
		}
		deps := map[string]bool{}
		for _, pr := range m.Properties {
			if depNameProps[pr.Name] {
				collectStrings(pr.Value, deps)
			}
		}
		out[m.Name()] = deps
	}
	return out
}

// aidlSiblings maps an aidl_interface name to every module defined in the same Android.bp
// (the interface itself, its translate/convert libraries, headers, tests) with their deps.
type aidlSiblings map[string]map[string]map[string]bool

func indexAidlInterfaces(outRoot string) aidlSiblings {
	idx := aidlSiblings{}
	for _, root := range aidlInterfaceRoots {
		walkFiles(filepath.Join(outRoot, root), isBlueprint, func(p string) {
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return
			}
			file, errs := parser.Parse(p, bytes.NewReader(b))
			if len(errs) > 0 {
				return
			}
			deps := bpModuleDeps(file)
			for _, def := range file.Defs {
				if m, ok := def.(*parser.Module); ok && m.Type == "aidl_interface" && m.Name() != "" {
					idx[m.Name()] = deps
				}
			}
		})
	}
	return idx
}

// aidlRepinMap returns old→new pin renames for one module's deps: a pin X-Vn-lang conflicts when
// the module also links a sibling of X whose own deps pin X at a different version.
func aidlRepinMap(deps map[string]bool, idx aidlSiblings) map[string]string {
	rename := map[string]string{}
	for dep := range deps {
		pin, ok := parseAidlPin(dep)
		if !ok {
			continue
		}
		sibs, ok := idx[pin.Iface]
		if !ok {
			continue
		}
		for sib, sdeps := range sibs {
			if sib == dep || !deps[sib] {
				continue
			}
			for sd := range sdeps {
				if sp, ok := parseAidlPin(sd); ok && sp.Iface == pin.Iface && sp.Lang == pin.Lang && sp.Ver != pin.Ver {
					rename[dep] = sp.String()
				}
			}
		}
	}
	return rename
}

func repinConflictingAidl(outRoot string, roots []string, r *compatReport) {
	idx := indexAidlInterfaces(outRoot)
	if len(idx) == 0 {
		r.Notes = append(r.Notes, "aidl pins: no aidl_interface definitions found under the target's interface trees — skipped")
		return
	}
	for _, root := range roots {
		walkFiles(filepath.Join(outRoot, root), isBlueprint, func(p string) {
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return
			}
			file, errs := parser.Parse(p, bytes.NewReader(b))
			if len(errs) > 0 {
				return
			}
			rename := map[string]string{}
			for _, deps := range bpModuleDeps(file) {
				for old, nw := range aidlRepinMap(deps, idx) {
					rename[old] = nw
				}
			}
			if len(rename) == 0 {
				return
			}
			out, ch, ferr := renameAndRepointBp(b, rename, "")
			if ferr != nil || !ch {
				return
			}
			if os.WriteFile(p, out, 0o644) != nil {
				return
			}
			rel, _ := filepath.Rel(outRoot, p)
			keys := make([]string, 0, len(rename))
			for k := range rename {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				r.AidlRepins = append(r.AidlRepins, filepath.ToSlash(rel)+": "+k+" → "+rename[k])
			}
		})
	}
}

// ─── runner ───

// applyTargetCompat runs the three operations over roots (repo-relative subtrees) and returns
// what changed. Order matters only for reporting.
// targetCompatOperations is how many operations applyTargetCompat runs; the README's numbered
// list and CURRENT_STATE are checked against it (docs_test.go).
const targetCompatOperations = 10

func applyTargetCompat(outRoot string, roots []string) compatReport {
	var r compatReport
	dropIllegalCflags(outRoot, roots, &r)
	moveSystemProps(outRoot, roots, &r)
	repinConflictingAidl(outRoot, roots, &r)
	replaceDenylistedAndroidMk(outRoot, roots, &r)
	addLostHeaderLibs(outRoot, roots, &r)
	renameProtoOptions(outRoot, roots, &r)
	dropPlatformDeclaredTypes(outRoot, roots, &r)
	dropNeverallowedStatements(outRoot, &r)
	dropWerrorUnderNewerClang(outRoot, roots, &r)
	pinFloatingAidlDefaults(outRoot, roots, &r)
	sort.Strings(r.WerrorFiles)
	sort.Strings(r.FlagFiles)
	sort.Strings(r.PropFiles)
	return r
}

func printCompatReport(r compatReport) {
	fmt.Println("\ntarget-release compatibility (probed on the target tree, applied to the mirrored subtrees only):")
	if len(r.IllegalFlags) > 0 {
		fmt.Printf("  illegal cflags per %s: %s\n", soongGlobalCflagsRel, strings.Join(r.IllegalFlags, " "))
	}
	for _, f := range r.FlagFiles {
		fmt.Printf("  ✓ dropped illegal cflag(s): %s\n", f)
	}
	for _, f := range r.PropFiles {
		fmt.Printf("  ✓ %s → %s: %s\n", systemPropsVar, productPropsVar, f)
	}
	for _, s := range r.AidlRepins {
		fmt.Printf("  ✓ aidl re-pin (sibling library conflict): %s\n", s)
	}
	for _, o := range r.Overlays {
		fmt.Printf("  ✓ Soong-conversion overlay: %s\n", o)
	}
	for _, f := range r.MkRemoved {
		fmt.Printf("      - removed denylisted %s\n", f)
	}
	for _, f := range r.OverlayFiles {
		fmt.Printf("      + wrote %s\n", f)
	}
	for _, h := range r.HeaderLibs {
		fmt.Printf("  ✓ %s\n", h)
	}
	for _, h := range r.ProtoRenames {
		fmt.Printf("  ✓ proto option: %s\n", h)
	}
	for _, h := range r.SepolicyDrops {
		fmt.Printf("  ✓ sepolicy: %s\n", h)
	}
	for _, f := range r.WerrorFiles {
		fmt.Printf("  ✓ dropped -Werror (newer clang): %s\n", f)
	}
	for _, h := range r.AidlPins {
		fmt.Printf("  ✓ aidl pin (vintf-declared version): %s\n", h)
	}
	for _, f := range r.BlockedMk {
		fmt.Printf("  ! %s is denylisted by the target's Soong and no overlay converts it — convert it to Android.bp (see %s)\n", f, androidmkDenylistRel)
	}
	for _, n := range r.Notes {
		fmt.Printf("  ~ %s\n", n)
	}
	if len(r.FlagFiles)+len(r.PropFiles)+len(r.AidlRepins)+len(r.Overlays)+len(r.HeaderLibs)+len(r.ProtoRenames)+len(r.SepolicyDrops)+len(r.WerrorFiles)+len(r.AidlPins) == 0 {
		fmt.Println("  = nothing to change")
	}
}

// ─── reference closure ───

// referencedGoogleSubtrees string-scans every makefile and Android.bp under dir for
// "device/google/<x>" and "hardware/google/<x>" path tokens and returns the distinct subtrees,
// sorted. The set is what the product chain, board config, Soong namespaces and visibility grants
// name — the "fully required" input to a mirror, derived from the data rather than a hand list.
// A token followed by a make variable (device/google/$(SOC)) yields no segment and is ignored.
func referencedGoogleSubtrees(dir string) []string {
	seen := map[string]bool{}
	isSeg := func(c byte) bool {
		return c == '_' || c == '-' || c == '.' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	walkFiles(dir, func(n string) bool { return strings.HasSuffix(n, ".mk") || n == "Android.bp" }, func(p string) {
		b, err := os.ReadFile(p)
		if err != nil {
			return
		}
		s := string(b)
		for _, pre := range []string{"device/google/", "hardware/google/"} {
			for from := 0; ; {
				i := strings.Index(s[from:], pre)
				if i < 0 {
					break
				}
				start := from + i + len(pre)
				end := start
				for end < len(s) && isSeg(s[end]) {
					end++
				}
				seg := strings.TrimRight(s[start:end], ".")
				if seg != "" {
					seen[pre+seg] = true
				}
				from = start
			}
		}
	})
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
