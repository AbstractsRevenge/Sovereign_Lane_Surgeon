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
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sepolicydecls.go — target-compat operation 7: SELinux type declarations the target's own
// system/sepolicy now makes.
//
// WHY (observed 2026-09-02, cheetah `m droid superimage`, Build Capture run 142532Z): checkpolicy
// fails with `Duplicate declaration of type` for vendor_chre_hal_prop — declared by
// device/google/gs-common/chre/sepolicy/property.te (android-15's vendor policy, via the
// vendor_internal_prop macro) AND by android-17's system/sepolicy/vendor/property.te, which took
// the CHRE property over. A type may be declared once; the platform's declaration is the one every
// other policy file in the tree is written against, so the mirrored declaration is dropped — only
// when system/sepolicy really declares the same name, and only that line (rules that USE the type,
// set_prop/get_prop/allow, stay) — unless the platform declares the type in private/, where
// vendor policy cannot see it: then the vendor rules on it and the file_contexts lines the
// platform already carries go too (per_boot_file: android-17's private kernel.te/toolbox.te/
// file_contexts hold the very rules gs101/gs201 shipped; run 143407Z "unknown type
// per_boot_file"). Scoped to types the pass itself un-declared, never a sweep over private
// types (system_ext policy dirs legitimately reference them). Measured on android-17: two types. Declaration forms recognized: `type X…;` and the te_macros property declarers
// `<something>_prop(X)`. Line scan, no regex (HARD RULE 3).

const systemSepolicyRel = "system/sepolicy"

// sepolicyDeclaredType returns the type a .te line declares, or "".
func sepolicyDeclaredType(line string) string {
	t := strings.TrimSpace(line)
	if i := strings.IndexByte(t, '#'); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	if strings.HasPrefix(t, "type ") {
		rest := strings.TrimSpace(t[len("type "):])
		end := 0
		for end < len(rest) && (rest[end] == '_' || (rest[end] >= 'a' && rest[end] <= 'z') || (rest[end] >= '0' && rest[end] <= '9')) {
			end++
		}
		if end > 0 && end < len(rest) && (rest[end] == ',' || rest[end] == ';' || rest[end] == ' ') {
			return rest[:end]
		}
		return ""
	}
	// <macro>_prop(X) — the property declarers in system/sepolicy/public/te_macros
	i := strings.Index(t, "_prop(")
	if i <= 0 || !strings.HasSuffix(t, ")") {
		return ""
	}
	for _, c := range t[:i] {
		if !(c == '_' || (c >= 'a' && c <= 'z')) {
			return ""
		}
	}
	name := t[i+len("_prop(") : len(t)-1]
	for _, c := range name {
		if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return ""
		}
	}
	return name
}

// sepolicyTypesUnder collects every type declared by .te files under dir.
func sepolicyTypesUnder(dir string) map[string]bool {
	out := map[string]bool{}
	walkFiles(dir, func(n string) bool { return strings.HasSuffix(n, ".te") }, func(p string) {
		b, err := os.ReadFile(p)
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(b), "\n") {
			if t := sepolicyDeclaredType(line); t != "" {
				out[t] = true
			}
		}
	})
	return out
}

// dropDeclaredTypes removes the declaration lines of the given types from a .te file's text.
func dropDeclaredTypes(content []byte, types map[string]bool) ([]byte, []string) {
	lines := strings.Split(string(content), "\n")
	kept := lines[:0]
	var dropped []string
	for _, l := range lines {
		if t := sepolicyDeclaredType(l); t != "" && types[t] {
			dropped = append(dropped, t)
			continue
		}
		kept = append(kept, l)
	}
	if len(dropped) == 0 {
		return content, nil
	}
	return []byte(strings.Join(kept, "\n")), dropped
}

// platformPrivateOnly returns, for the given platform-declared types, those declared under
// system/sepolicy/private and nowhere vendor policy can see (public/, vendor/).
func platformPrivateOnly(outRoot string, types map[string]bool) map[string]bool {
	sp := filepath.Join(outRoot, filepath.FromSlash(systemSepolicyRel))
	visible := sepolicyTypesUnder(filepath.Join(sp, "public"))
	for t := range sepolicyTypesUnder(filepath.Join(sp, "vendor")) {
		visible[t] = true
	}
	private := sepolicyTypesUnder(filepath.Join(sp, "private"))
	out := map[string]bool{}
	for t := range types {
		if private[t] && !visible[t] {
			out[t] = true
		}
	}
	return out
}

// hasToken reports whether s contains name as a whole identifier token.
func hasToken(s, name string) bool {
	isID := func(c byte) bool {
		return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
	}
	for from := 0; ; {
		i := strings.Index(s[from:], name)
		if i < 0 {
			return false
		}
		i += from
		j := i + len(name)
		if (i == 0 || !isID(s[i-1])) && (j == len(s) || !isID(s[j])) {
			return true
		}
		from = i + 1
	}
}

// dropStatementsUsing removes every policy statement that mentions one of the types: a statement
// is a line, or a run of lines up to the one ending in ';' when an earlier line opened it (a rule
// with a multi-line permission set). Comments and blank lines are kept.
func dropStatementsUsing(content []byte, types map[string]bool) ([]byte, int) {
	lines := strings.Split(string(content), "\n")
	var kept []string
	dropped := 0
	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "#") {
			kept = append(kept, lines[i])
			continue
		}
		// gather a statement
		end := i
		if !strings.HasSuffix(t, ";") && !strings.HasSuffix(t, ")") {
			for end+1 < len(lines) && !strings.HasSuffix(strings.TrimSpace(lines[end]), ";") {
				end++
			}
		}
		stmt := strings.Join(lines[i:end+1], "\n")
		if code := stmt; func() bool {
			if k := strings.Index(code, "#"); k >= 0 {
				code = code[:k]
			}
			for ty := range types {
				if hasToken(code, ty) {
					return true
				}
			}
			return false
		}() {
			dropped++
		} else {
			kept = append(kept, lines[i:end+1]...)
		}
		i = end
	}
	if dropped == 0 {
		return content, 0
	}
	return []byte(strings.Join(kept, "\n")), dropped
}

// platformFileContexts returns the "<pathspec> <label>" pairs of the platform's private
// file_contexts, keyed by path spec.
func platformFileContexts(outRoot string) map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(filepath.Join(outRoot, filepath.FromSlash(systemSepolicyRel), "private", "file_contexts"))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && !strings.HasPrefix(f[0], "#") {
			out[f[0]] = f[len(f)-1]
		}
	}
	return out
}

// dropFileContextsLabelled removes file_contexts lines labelled with one of the types when the
// platform's private file_contexts already labels the same path spec with the same type.
func dropFileContextsLabelled(content []byte, types map[string]bool, platform map[string]string) ([]byte, int) {
	lines := strings.Split(string(content), "\n")
	kept := lines[:0]
	dropped := 0
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) >= 2 && !strings.HasPrefix(f[0], "#") {
			label := f[len(f)-1]
			parts := strings.Split(label, ":")
			if len(parts) >= 3 && types[parts[2]] && platform[f[0]] == label {
				dropped++
				continue
			}
		}
		kept = append(kept, l)
	}
	if dropped == 0 {
		return content, 0
	}
	return []byte(strings.Join(kept, "\n")), dropped
}

// sepolicyTypesInFS collects the types the PRISTINE mirrored policy declares under roots in fsys
// (the embedded bundle, or a -source-root): the candidate set has to come from the source, not
// from the target's copy, or a re-run over an already-treated tree — declarations gone, rules
// still there — would find nothing to key on.
func sepolicyTypesInFS(fsys fs.FS, roots []string) map[string]bool {
	out := map[string]bool{}
	for _, root := range roots {
		_ = fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".te") {
				return nil
			}
			b, rerr := fs.ReadFile(fsys, p)
			if rerr != nil {
				return nil
			}
			for _, line := range strings.Split(string(b), "\n") {
				if t := sepolicyDeclaredType(line); t != "" {
					out[t] = true
				}
			}
			return nil
		})
	}
	return out
}

func dropPlatformDeclaredTypes(outRoot string, roots []string, r *compatReport) {
	dropPlatformDeclaredTypesFrom(outRoot, roots, embeddedFS, r)
}

func dropPlatformDeclaredTypesFrom(outRoot string, roots []string, srcFS fs.FS, r *compatReport) {
	platform := sepolicyTypesUnder(filepath.Join(outRoot, filepath.FromSlash(systemSepolicyRel)))
	if len(platform) == 0 {
		r.Notes = append(r.Notes, "sepolicy: no "+systemSepolicyRel+" under the target — declarations left as mirrored")
		return
	}
	// Candidates: types the pristine mirrored policy declares that the platform also declares.
	undeclared := map[string]bool{}
	for t := range sepolicyTypesInFS(srcFS, roots) {
		if platform[t] {
			undeclared[t] = true
		}
	}
	if len(undeclared) == 0 {
		return
	}
	// Pass 1: declarations.
	for _, root := range roots {
		walkFiles(filepath.Join(outRoot, filepath.FromSlash(root)), func(n string) bool { return strings.HasSuffix(n, ".te") }, func(p string) {
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return
			}
			out, dropped := dropDeclaredTypes(b, undeclared)
			if len(dropped) == 0 {
				return
			}
			if os.WriteFile(p, out, 0o644) != nil {
				return
			}
			sort.Strings(dropped)
			rel, _ := filepath.Rel(outRoot, p)
			r.SepolicyDrops = append(r.SepolicyDrops, filepath.ToSlash(rel)+": dropped declaration of "+strings.Join(dropped, ", ")+" (declared by the target's "+systemSepolicyRel+")")
		})
	}
	// Pass 2: a type the platform declares PRIVATELY is invisible to vendor policy, so every
	// mirrored rule on it ("unknown type", run 143407Z) and every file_contexts line the platform
	// already carries for it must go too — the platform's own private policy holds them now.
	private := platformPrivateOnly(outRoot, undeclared)
	if len(private) == 0 {
		return
	}
	pfc := platformFileContexts(outRoot)
	for _, root := range roots {
		walkFiles(filepath.Join(outRoot, filepath.FromSlash(root)), func(n string) bool { return strings.HasSuffix(n, ".te") || n == "file_contexts" }, func(p string) {
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return
			}
			var out []byte
			var n int
			if filepath.Base(p) == "file_contexts" {
				out, n = dropFileContextsLabelled(b, private, pfc)
			} else {
				out, n = dropStatementsUsing(b, private)
			}
			if n == 0 || os.WriteFile(p, out, 0o644) != nil {
				return
			}
			rel, _ := filepath.Rel(outRoot, p)
			names := make([]string, 0, len(private))
			for t := range private {
				names = append(names, t)
			}
			sort.Strings(names)
			r.SepolicyDrops = append(r.SepolicyDrops, filepath.ToSlash(rel)+": dropped "+itoa(n)+" line(s) using "+strings.Join(names, ", ")+" (platform-private now; the platform carries these rules)")
		})
	}
}
