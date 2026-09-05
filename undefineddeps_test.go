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
	"os"
	"path/filepath"
	"testing"
)

// TestUndefinedDeps pins the census semantics: a lane dep is undefined unless a lane bp, a non-stock
// bp, or a stock bp WITHOUT a lane parallel defines it; a stock bp shadowed by a lane parallel does
// not count (the finder drops it); a manifest-dropped bp does not count.
func TestUndefinedDeps(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, body string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	mk("frameworks-holo/base/Android.bp", "java_library {\n    name: \"lane_lib\",\n    static_libs: [\"present\", \"gone\", \"shadowed\", \"dropped\", \"lane_lib2\", \"included\"],\n}\njava_library {\n    name: \"lane_lib2\",\n}\n")
	mk("frameworks/base/Android.bp", "java_library {\n    name: \"stock_lib\",\n}\n")            // has a lane parallel: dropped by the finder
	mk("frameworks/libs/x/Android.bp", "java_library {\n    name: \"present\",\n}\n")            // no lane parallel: loads
	mk("frameworks/libs/y/Android.bp", "java_library {\n    name: \"shadowed\",\n}\n")           // lane parallel exists but defines something else
	mk("frameworks-holo/libs/y/Android.bp", "java_library {\n    name: \"shadowed-holo\",\n}\n") // the Holo rename case
	mk("system/z/Android.bp", "java_library {\n    name: \"dropped\",\n}\n")                     // route-manifest drop
	mk("frameworks/libs/w/Android.bp", "build = [\"Flags.bp\"]\n")                               // pulls Flags.bp into the package
	mk("frameworks/libs/w/Flags.bp", "java_library {\n    name: \"included\",\n}\n")
	mk(".holo/holo_bp_route_manifest.json", "{\"dropped_namespace_decl_paths\": [\"system/z/Android.bp\"]}\n")
	rep := undefinedDeps(root, "holo")
	got := map[string]int{}
	for _, r := range rep {
		got[r.Name] = len(r.Bps)
	}
	for _, want := range []string{"gone", "shadowed", "dropped"} {
		if got[want] != 1 {
			t.Errorf("%q: want reported once, got %d (report %v)", want, got[want], got)
		}
	}
	for _, dontWant := range []string{"present", "lane_lib2", "stock_lib", "included"} {
		if _, ok := got[dontWant]; ok {
			t.Errorf("%q must not be reported as undefined", dontWant)
		}
	}
}
