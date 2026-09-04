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
	"strings"
	"testing"
)

const optsProto17 = "extend google.protobuf.FieldOptions {\n    optional bool is_uid = 50001 [default = false];\n    // reserved 50004; module has been moved to module_name\n    repeated string module_name = 50010;\n}\n"
const optsProto15 = "extend google.protobuf.FieldOptions {\n    optional bool is_uid = 50001 [default = false];\n    repeated string module = 50004;\n}\n"
const pixelProto = "import \"frameworks/proto_logging/stats/atom_field_options.proto\";\nmessage Atom {\n  oneof pushed {\n    A a = 105019 [(android.os.statsd.module) = \"pixelaudio\"];\n    B b = 105022 [(android.os.statsd.is_uid) = true, (android.os.statsd.module) = \"pixelstats\"];\n  }\n}\n"

func TestRenameProtoOptionsGatedOnTarget(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"frameworks/proto_logging/stats/atom_field_options.proto": optsProto17,
		"hardware/google/pixel/pixelstats/pixelatoms.proto":       pixelProto,
	})
	var r compatReport
	renameProtoOptions(root, []string{"hardware/google/pixel"}, &r)
	if len(r.ProtoRenames) != 1 || !strings.Contains(r.ProtoRenames[0], "×2") {
		t.Fatalf("report %v", r.ProtoRenames)
	}
	b, _ := os.ReadFile(filepath.Join(root, "hardware/google/pixel/pixelstats/pixelatoms.proto"))
	s := string(b)
	if strings.Contains(s, "statsd.module)") || strings.Count(s, "(android.os.statsd.module_name)") != 2 || !strings.Contains(s, "(android.os.statsd.is_uid)") {
		t.Fatalf("bad rewrite:\n%s", s)
	}
	var r2 compatReport
	renameProtoOptions(root, []string{"hardware/google/pixel"}, &r2)
	if len(r2.ProtoRenames) != 0 {
		t.Fatal("not idempotent")
	}
	// android-15 style target: untouched
	root2 := t.TempDir()
	writeTree(t, root2, map[string]string{
		"frameworks/proto_logging/stats/atom_field_options.proto": optsProto15,
		"hardware/google/pixel/pixelstats/pixelatoms.proto":       pixelProto,
	})
	var r3 compatReport
	renameProtoOptions(root2, []string{"hardware/google/pixel"}, &r3)
	if len(r3.ProtoRenames) != 0 {
		t.Fatalf("a target that still defines the old option must leave the proto alone: %v", r3.ProtoRenames)
	}
}
