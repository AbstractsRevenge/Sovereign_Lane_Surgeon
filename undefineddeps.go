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
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	parser "github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/internal/blueprint/parser"
)

// undefineddeps.go — the `undefined-deps` diagnostic: every dependency name a lane's Android.bp
// files reference that NOTHING in the tree will define once the finder has routed the lane.
//
// Why it exists (android-17 Holo landing, 2026-09-05): Soong reports "depends on undefined module"
// one or two at a time, and each report costs a full analysis run (~1-4 min). Landing a lane whose
// delta came from another Android version surfaces a CLASS of them at once — prebuilts the source
// tree carried outside the lane (com.google.android.holo_holo, the Holo fork of the Material
// Components AAR under prebuilts/), modules a conflicted bp still names by its old name, libraries
// the lane renamed. One AST census (the vendored Blueprint parser, the same depNamesInBp / bpModules
// the re-export idiom uses) lists them all up front, attributed to the bps that reference them.
//
// What "will define" means: a module is defined if it is declared in (a) any lane bp, (b) any
// non-stock bp, or (c) a stock frameworks/ or packages/ bp whose LANE PARALLEL DOES NOT EXIST —
// the keep-name finder drops the stock parallel of every lane bp — minus any bp the lane's route
// manifest drops. Labels (//dir:name) resolve by path and are not names; depNamesInBp skips them.
// Diagnostic only: prints and exits 0 (-fail makes an empty result the only zero exit).

// undefinedDepsReport is one undefined name and the lane bps that reference it.
type undefinedDepsReport struct {
	Name string
	Bps  []string
}

// laneBpRoots lists the directories whose Android.bp files belong to the lane.
func laneBpRoots(outRoot, lane string) []string {
	roots := []string{"frameworks-" + lane, "packages-" + lane}
	if ents, err := os.ReadDir(filepath.Join(outRoot, "device", "google")); err == nil {
		for _, e := range ents {
			if e.IsDir() && strings.HasSuffix(e.Name(), "-"+lane) {
				roots = append(roots, filepath.Join("device", "google", e.Name()))
			}
		}
	}
	return roots
}

// manifestDrops reads the lane's route manifest dropped list (absent manifest = no drops).
func manifestDrops(outRoot, lane string) map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(filepath.Join(outRoot, "."+lane, lane+"_bp_route_manifest.json"))
	if err != nil {
		return out
	}
	var m struct {
		Dropped []string `json:"dropped_namespace_decl_paths"`
	}
	if json.Unmarshal(b, &m) == nil {
		for _, d := range m.Dropped {
			out[d] = true
		}
	}
	return out
}

// definingSourceRoots is every top-level dir whose Android.bp files the finder loads on a lane
// lunch: all of them except build outputs, hidden/underscore dirs, and SIBLING lanes (a lane build
// is blind to them). Unlike survivingSourceRoots it keeps prebuilts/ and toolchain/: they define
// modules (androidx.*, com.google.android.material_material, ...) even though they are never
// re-export consumers.
func definingSourceRoots(outRoot, lane string) []string {
	entries, err := os.ReadDir(outRoot)
	if err != nil {
		return nil
	}
	var roots []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, "out") || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_") {
			continue
		}
		if (strings.HasPrefix(n, "frameworks-") || strings.HasPrefix(n, "packages-") || strings.HasPrefix(n, "external-")) &&
			n != "frameworks-"+lane && n != "packages-"+lane && n != "external-"+lane {
			continue // sibling lane
		}
		roots = append(roots, n)
	}
	return roots
}

// derivedSuffixes are the name derivations Soong module types generate from a declared base name
// and that no Android.bp ever declares: java_sdk_library stubs (X.stubs, X.stubs.system, X.impl,
// X.stubs.module_lib, X.stubs.system_server, X.stubs.test), aconfig_declarations libraries
// (X-aconfig-java, X-aconfig-java-export, X-aconfig-java-host), aidl_interface backends
// (X-cpp, X-ndk, X-java, X-rust, X-V<n>-cpp ...), and versioned NDK/C++ variants. A reference to
// one of these is satisfied if its BASE is declared; a static census cannot know more without
// running Soong, and pretending otherwise would bury the real gaps under hundreds of false ones.
var derivedSuffixes = []string{
	".stubs.module_lib", ".stubs.system_server", ".stubs.system", ".stubs.test", ".stubs", ".impl",
	"-aconfig-java-export", "-aconfig-java-host", "-aconfig-java", "-aconfig-cc", "-aconfig-rust",
	"-cpp", "-ndk", "-java", "-rust", "-cpp-analyzer", "-cpp-shared", "-ndk-shared",
	"-compiler-plugin", "-nodeps", "_static", "-static", ".ravenwood", "-host", "-source", "-java-source",
	"-V1.0-java", "-V1.1-java", "-V1.2-java", "-V1.3-java", "-V2.0-java", "-V2.1-java", "-V1.0-ndk", "-V1.0-cpp",
	"-V1.1-ndk", "-V2.0-ndk", "-V1.0-adapter-helper", "-V1.1-adapter-helper",
}

// generatedFamilies are module-name families Soong SINGLETONS synthesize (combined_apis, prebuilt_apis,
// the API-stubs aggregation) with no declaring bp anywhere; a static census lists them as
// "generated" rather than undefined. Prefix match.
var generatedFamilies = []string{
	"all-modules-", "all-updatable-modules-", "all-non-updatable-modules-", "all-framework-module-",
	"android-non-updatable", "android-incompatibilities", "android_stubs_current", "android_system_stubs_current",
	"android_test_stubs_current", "android_module_lib_stubs_current", "android_system_server_stubs_current",
}

// definedOrDerived reports whether name is declared, or derives from a declared base.
func definedOrDerived(name string, defined map[string]bool) bool {
	if defined[name] {
		return true
	}
	for _, f := range generatedFamilies {
		if strings.HasPrefix(name, f) {
			return true
		}
	}
	if strings.Contains(name, ".api.") && strings.HasSuffix(name, ".latest") {
		return true // prebuilt_apis-generated
	}
	n := name
	// hidl_interface "X@1.0" generates X-V1.0-java / -cpp / -ndk / -java-constants; sysprop_library
	// "Foo" generates libFoo (cc) and Foo-java. Map back to the declared name.
	if k := strings.Index(n, "-V"); k > 0 {
		rest := n[k+2:]
		if j := strings.Index(rest, "-"); j > 0 && strings.Trim(rest[:j], "0123456789.") == "" {
			if defined[n[:k]+"@"+rest[:j]] {
				return true
			}
		}
	}
	if strings.HasPrefix(n, "lib") && strings.HasSuffix(n, "Properties") && defined[strings.TrimPrefix(n, "lib")] {
		return true
	}
	if k := strings.Index(n, "{"); k > 0 {
		n = n[:k] // output tag: X{.aapt.srcjar}
	}
	if k := strings.Index(n, "#"); k > 0 {
		n = n[:k] // NDK/API-versioned ref: libandroid#31
	}
	if defined[n] {
		return true
	}
	for i := 0; i < 4; i++ {
		stripped := false
		for _, suf := range derivedSuffixes {
			if strings.HasSuffix(n, suf) && len(n) > len(suf) {
				n = strings.TrimSuffix(n, suf)
				stripped = true
				break
			}
		}
		// aidl_interface versioned backends: X-V3-ndk -> X ; X-V3 -> X
		if k := strings.LastIndex(n, "-V"); k > 0 {
			rest := n[k+2:]
			if rest != "" && strings.Trim(rest, "0123456789") == "" {
				n = n[:k]
				stripped = true
			}
		}
		if defined[n] {
			return true
		}
		if !stripped {
			break
		}
	}
	return false
}

// bpIncludedFiles returns the extra .bp files an Android.bp pulls in through its top-level
// `build = ["X.bp", ...]` assignment (frameworks/base/Android.bp includes AconfigFlags.bp this way,
// and that is where 17 declares every java_aconfig_library). Blueprint loads them as part of the
// same package, so a definition census that reads only Android.bp misses them.
func bpIncludedFiles(androidBp string) []string {
	b, err := os.ReadFile(androidBp)
	if err != nil {
		return nil
	}
	file, errs := parser.Parse("", bytes.NewReader(b))
	if len(errs) > 0 {
		return nil
	}
	var out []string
	for _, def := range file.Defs {
		as, ok := def.(*parser.Assignment)
		if !ok || as.Name != "build" {
			continue
		}
		if l, ok := as.Value.(*parser.List); ok {
			for _, el := range l.Values {
				if s, ok := el.(*parser.String); ok && s.Value != "" {
					out = append(out, filepath.Join(filepath.Dir(androidBp), s.Value))
				}
			}
		}
	}
	return out
}

// undefinedDeps computes the census for one lane under outRoot.
func undefinedDeps(outRoot, lane string) []undefinedDepsReport {
	refs := map[string]map[string]bool{} // dep name -> referencing lane bps
	for _, root := range laneBpRoots(outRoot, lane) {
		filepath.Walk(filepath.Join(outRoot, root), func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
				return nil
			}
			into := map[string]bool{}
			for _, bp := range append([]string{p}, bpIncludedFiles(p)...) {
				depNamesInBpOpt(bp, into, false)
			}
			rel, _ := filepath.Rel(outRoot, p)
			for n := range into {
				if refs[n] == nil {
					refs[n] = map[string]bool{}
				}
				refs[n][rel] = true
			}
			return nil
		})
	}
	drops := manifestDrops(outRoot, lane)
	defined := map[string]bool{}
	for _, top := range definingSourceRoots(outRoot, lane) {
		filepath.Walk(filepath.Join(outRoot, top), func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() || filepath.Base(p) != "Android.bp" {
				return nil
			}
			rel, _ := filepath.Rel(outRoot, p)
			if drops[rel] {
				return nil
			}
			for _, sr := range []string{"frameworks", "packages"} {
				if strings.HasPrefix(rel, sr+"/") {
					if _, err := os.Stat(filepath.Join(outRoot, sr+"-"+lane, strings.TrimPrefix(rel, sr+"/"))); err == nil {
						return nil // stock parallel of a lane bp: the finder drops it
					}
				}
			}
			for _, bp := range append([]string{p}, bpIncludedFiles(p)...) {
				names, _ := bpModules(bp)
				for n := range names {
					defined[n] = true
				}
			}
			return nil
		})
	}
	var out []undefinedDepsReport
	for n, bps := range refs {
		if definedOrDerived(n, defined) {
			continue
		}
		list := make([]string, 0, len(bps))
		for b := range bps {
			list = append(list, b)
		}
		sort.Strings(list)
		out = append(out, undefinedDepsReport{Name: n, Bps: list})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Bps) != len(out[j].Bps) {
			return len(out[i].Bps) > len(out[j].Bps)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// cmdUndefinedDeps is the `undefined-deps` subcommand.
func cmdUndefinedDeps(args []string) int {
	fs := flag.NewFlagSet("undefined-deps", flag.ExitOnError)
	name := fs.String("name", "", "lane name (e.g. holo)")
	out := fs.String("out", "", "AOSP root")
	fail := fs.Bool("fail", false, "exit 1 when any undefined dependency is found (gate mode)")
	_ = fs.Parse(args)
	if *name == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "undefined-deps: -name and -out are required")
		return 2
	}
	rep := undefinedDeps(*out, *name)
	fmt.Printf("undefined-deps: %d dependency name(s) referenced by lane %q bps that nothing will define:\n", len(rep), *name)
	for _, r := range rep {
		ex := r.Bps
		if len(ex) > 3 {
			ex = append(append([]string{}, ex[:3]...), fmt.Sprintf("... +%d", len(r.Bps)-3))
		}
		fmt.Printf("  %-56s %3d bp  %s\n", r.Name, len(r.Bps), strings.Join(ex, ", "))
	}
	if *fail && len(rep) > 0 {
		return 1
	}
	return 0
}
