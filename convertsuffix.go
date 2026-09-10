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
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	parser "github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/internal/blueprint/parser"
)

// convertsuffix.go — the `convert-suffix` subcommand: convert an EXISTING prefix-renamed rename lane to
// the SUFFIX model IN PLACE, without re-seeding.
//
// WHY a separate driver. runRenameInstallables/runRenameLibs are FORWARD-ONLY (stock name → lane name on
// a fresh clone) and idempotent via a prefix/suffix guard, so they cannot convert a lane already carrying
// <Prefix>-named modules: a suffix re-run over NexusMSettings would double it to NexusMSettingsNexus, not
// SettingsNexus. And re-seed is often out — an established lane carries hand-authored content (e.g. a
// re-authored SystemUI) a re-clone from stock would not reproduce. This bridges the gap: STRIP the current
// prefix, APPEND the suffix — apps <base><Camel> (PascalCase), libs/filegroups <base>_<lower> (snake).
//
// The strip token and the append token may DIFFER — a lane rename+migration (NexusM-prefix → Nexus-suffix)
// — or be equal for a pure model flip (Holo2-prefix → Holo2-suffix). Framework-class modules already carry
// a <base>-<lane> DASH suffix (not the Camel prefix) so they are left alone here; a lane-token rename of
// those (nexusm→nexus) is a separate rename-module map. Re-uses renameAndRepointBp, so every dep name,
// :module / //path:module label, and derived-aidl form (X-cpp/-rust/-V<n>) repoints in lockstep. Idempotent
// (a converted module no longer carries the strip prefix). Dry-run by default; -apply writes.
func cmdConvertSuffix(args []string) int {
	fset := flag.NewFlagSet("convert-suffix", flag.ExitOnError)
	name := fset.String("name", "", "lane DIR name — walks frameworks-<name>/ and packages-<name>/")
	out := fset.String("out", "", "AOSP root")
	strip := fset.String("strip", "", "comma-separated CURRENT prefix(es) to strip (e.g. Holo2 | NexusM,Nexusm). Default: -camel")
	camelF := fset.String("camel", "", "new PascalCase app suffix (e.g. Holo2, Nexus). Default: Title(name)")
	lowerF := fset.String("lower", "", "new lowercase lib/filegroup suffix (e.g. holo2, nexus). Default: name")
	apply := fset.Bool("apply", false, "apply the conversion (default: dry-run — print the plan)")
	_ = fset.Parse(args)
	if *name == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "convert-suffix: -name <lane> and -out <aosp-root> are required")
		return 2
	}
	camel := *camelF
	if camel == "" {
		camel = strings.ToUpper((*name)[:1]) + (*name)[1:]
	}
	lower := *lowerF
	if lower == "" {
		lower = *name
	}
	var stripPfx []string
	if *strip == "" {
		stripPfx = []string{camel}
	} else {
		for _, s := range strings.Split(*strip, ",") {
			if s = strings.TrimSpace(s); s != "" {
				stripPfx = append(stripPfx, s)
			}
		}
	}

	rename := collectPrefixToSuffixRenames(*out, *name, stripPfx, camel, lower)
	fmt.Printf("convert-suffix: lane=%s strip=%v → apps <base>%s, libs <base>_%s — %d module(s)\n",
		*name, stripPfx, camel, lower, len(rename))
	if len(rename) == 0 {
		fmt.Println("  (no prefix-renamed app/lib modules found — already suffix, or wrong -strip)")
		return 0
	}
	shown := 0
	for k, v := range rename {
		if shown >= 15 {
			break
		}
		fmt.Printf("    %s → %s\n", k, v)
		shown++
	}
	if len(rename) > shown {
		fmt.Printf("    … and %d more\n", len(rename)-shown)
	}

	changed, failed := 0, 0
	for _, root := range []string{"frameworks-" + *name, "packages-" + *name} {
		rootDir := filepath.Join(*out, root)
		if fi, err := os.Stat(rootDir); err != nil || !fi.IsDir() {
			continue
		}
		filepath.Walk(rootDir, func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || !isBpFile(filepath.Base(p)) {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			outb, ch, ferr := renameAndRepointBp(b, rename, *name)
			if ferr != nil {
				failed++
				return nil
			}
			if ch {
				changed++
				if *apply {
					_ = os.WriteFile(p, outb, 0o644)
				}
			}
			return nil
		})
	}

	// Device .mk PRODUCT_PACKAGES et al. reference identity apps by their CURRENT (prefix) name — bp-only
	// renaming would leave those dangling. Apply the SAME map to device/*.mk: match prefix tokens, look
	// each up, replace only mapped ones (a Holo2<X> the bp pass did NOT rename — e.g. a non-app/lib
	// prebuilt — keeps its name in both bp and mk, so it stays consistent). Word-boundaried, so
	// Holo2Settings never eats Holo2SettingsProvider.
	mkFiles, mkRefs := applyRenameMapToMk(filepath.Join(*out, "device"), stripPfx, rename, *apply)

	verb, tail := "would update", "(dry-run — re-run with -apply to write)"
	if *apply {
		verb, tail = "updated", "APPLIED."
	}
	fmt.Printf("\nconvert-suffix: %s %d bp + %d device .mk (%d mk ref(s)), %d skipped (parse). %s\n",
		verb, changed, mkFiles, mkRefs, failed, tail)
	return 0
}

// applyRenameMapToMk applies the prefix→suffix rename map to Make files under root (device/*.mk), where
// identity apps are named in PRODUCT_PACKAGES etc. Only tokens carrying a strip prefix are considered, and
// only those present in the map are rewritten — a prefix token the bp pass left alone stays put (its bp
// name is unchanged, so the mk ref remains valid). Returns (files changed, refs rewritten).
func applyRenameMapToMk(root string, stripPrefixes []string, rename map[string]string, apply bool) (files, refs int) {
	if len(rename) == 0 {
		return 0, 0
	}
	alt := make([]string, 0, len(stripPrefixes))
	for _, p := range stripPrefixes {
		if p != "" {
			alt = append(alt, regexp.QuoteMeta(p))
		}
	}
	if len(alt) == 0 {
		return 0, 0
	}
	// A prefix-rooted module token: <prefix> then module-name chars (Soong names allow . - _).
	tokRe := regexp.MustCompile(`\b(?:` + strings.Join(alt, "|") + `)[A-Za-z0-9._-]*`)
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return 0, 0
	}
	filepath.Walk(root, func(p string, fi os.FileInfo, e error) error {
		if e != nil || fi.IsDir() || !strings.HasSuffix(p, ".mk") {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		n := 0
		out := tokRe.ReplaceAllStringFunc(string(b), func(tok string) string {
			if nv, ok := rename[tok]; ok {
				n++
				return nv
			}
			return tok
		})
		if n == 0 {
			return nil
		}
		files++
		refs += n
		if apply {
			_ = os.WriteFile(p, []byte(out), fi.Mode().Perm())
		}
		return nil
	})
	return files, refs
}

// collectPrefixToSuffixRenames builds the old(prefix)→new(suffix) map for a prefix-renamed lane.
// stripPrefixes are the current prefixes to strip (["Holo2"] | ["NexusM","Nexusm"]); camel/lower are the
// new suffix tokens. Only app/lib modules currently carrying a strip prefix are mapped — framework-class
// (<base>-<lane> dash-suffix), keep-name, and already-suffix modules carry no strip prefix and are skipped.
func collectPrefixToSuffixRenames(outRoot, lane string, stripPrefixes []string, camel, lower string) map[string]string {
	m := map[string]string{}
	for _, root := range []string{"frameworks-" + lane, "packages-" + lane} {
		rootDir := filepath.Join(outRoot, root)
		if fi, err := os.Stat(rootDir); err != nil || !fi.IsDir() {
			continue
		}
		filepath.Walk(rootDir, func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || !isBpFile(filepath.Base(p)) {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			file, errs := parser.Parse("", bytes.NewReader(b))
			if len(errs) > 0 {
				return nil
			}
			for _, def := range file.Defs {
				mod, ok := def.(*parser.Module)
				if !ok {
					continue
				}
				isApp := installableAppTypes[mod.Type]
				isLib := libRenameTypes[mod.Type]
				if !isApp && !isLib {
					continue
				}
				for _, pr := range mod.Properties {
					if pr.Name != "name" {
						continue
					}
					ns, ok := pr.Value.(*parser.String)
					if !ok {
						continue
					}
					n := ns.Value
					base := ""
					for _, pfx := range stripPrefixes {
						if pfx != "" && strings.HasPrefix(n, pfx) && len(n) > len(pfx) {
							base = n[len(pfx):]
							break
						}
					}
					if base == "" || frameworkClassNames[base] {
						continue
					}
					suffixed := base + camel
					if !isApp {
						suffixed = base + "_" + lower
					}
					if suffixed != n {
						m[n] = suffixed
					}
				}
			}
			return nil
		})
	}
	return m
}
