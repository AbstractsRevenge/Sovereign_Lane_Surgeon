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
	_ "embed"
	"os"
	"path/filepath"
	"strings"
)

// protooptions.go — target-compat operation 6: a proto extension the target renamed.
//
// WHY (observed 2026-09-02, cheetah `m droid superimage`, Build Capture run 140831Z): aprotoc
// rejects hardware/google/pixel/pixelstats/pixelatoms.proto with `Option "(android.os.statsd.module)"
// unknown` 26 times. android-17's frameworks/proto_logging/stats/atom_field_options.proto says why
// in its own text — "reserved 50004; module has been moved to module_name" — and defines
// `repeated string module_name = 50010;`, which its own atoms use as `[(module_name) = "…"]`. The
// mirrored proto is android-15's and still writes the old name. The table names such renames; each
// is applied to the mirrored .proto files only when the target's defining proto really declares the
// new field and no longer declares the old one (line scan of that proto, no regex).
type renamedProtoOption struct {
	DefProto string // repo-relative proto that defines the extension
	Package  string // proto package of the extension
	Old, New string
}

//go:embed assets/compat/proto_options.MANIFEST
var embeddedProtoOptionsManifest string

func parseProtoOptions(text string) []renamedProtoOption {
	var out []renamedProtoOption
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 4 {
			continue
		}
		out = append(out, renamedProtoOption{DefProto: f[0], Package: f[1], Old: f[2], New: f[3]})
	}
	return out
}

// The rows live in assets/compat/proto_options.MANIFEST (data, not code).
var renamedProtoOptions = parseProtoOptions(embeddedProtoOptionsManifest)

// protoDeclaresField reports whether the proto text declares a field named name
// (`… string name = N;` on one line, comments stripped).
func protoDeclaresField(content []byte, name string) bool {
	for _, line := range strings.Split(string(content), "\n") {
		t := strings.TrimSpace(line)
		if i := strings.Index(t, "//"); i >= 0 {
			t = strings.TrimSpace(t[:i])
		}
		if !strings.HasSuffix(t, ";") {
			continue
		}
		f := strings.Fields(t)
		for i := 0; i+2 < len(f); i++ {
			if f[i] == name && f[i+1] == "=" {
				return true
			}
		}
	}
	return false
}

func targetRenamedOption(outRoot string, r renamedProtoOption) bool {
	b, err := os.ReadFile(filepath.Join(outRoot, filepath.FromSlash(r.DefProto)))
	if err != nil {
		return false
	}
	return protoDeclaresField(b, r.New) && !protoDeclaresField(b, r.Old)
}

// renameProtoOption replaces the exact option token "(pkg.old)" with "(pkg.new)"; the parentheses
// make the token unambiguous, so no other identifier can match.
func renameProtoOption(content []byte, r renamedProtoOption) ([]byte, int) {
	old := []byte("(" + r.Package + "." + r.Old + ")")
	nw := []byte("(" + r.Package + "." + r.New + ")")
	n := bytes.Count(content, old)
	if n == 0 {
		return content, 0
	}
	return bytes.ReplaceAll(content, old, nw), n
}

func renameProtoOptions(outRoot string, roots []string, rep *compatReport) {
	for _, r := range renamedProtoOptions {
		if !targetRenamedOption(outRoot, r) {
			continue
		}
		for _, root := range roots {
			walkFiles(filepath.Join(outRoot, filepath.FromSlash(root)), func(n string) bool { return strings.HasSuffix(n, ".proto") }, func(p string) {
				b, rerr := os.ReadFile(p)
				if rerr != nil {
					return
				}
				out, n := renameProtoOption(b, r)
				if n == 0 {
					return
				}
				if os.WriteFile(p, out, 0o644) == nil {
					rel, _ := filepath.Rel(outRoot, p)
					rep.ProtoRenames = append(rep.ProtoRenames, filepath.ToSlash(rel)+": ("+r.Package+"."+r.Old+") → ("+r.Package+"."+r.New+") ×"+itoa(n)+" (the target's "+filepath.Base(r.DefProto)+" moved it)")
				}
			})
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
