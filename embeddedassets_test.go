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
	"testing"
)

// The manifest must describe the assets tree that is actually embedded: a script added to
// assets/ with +x but without `go generate` would silently materialize as 0644 again.
func TestExecManifestMatchesAssetsTree(t *testing.T) {
	if _, err := os.Stat(embeddedAssetsRoot); err != nil {
		t.Skip("assets tree not on disk (test not run from the repository root)")
	}
	var onDisk []string
	err := filepath.WalkDir(embeddedAssetsRoot, func(p string, d fs.DirEntry, e error) error {
		if e != nil || d.IsDir() {
			return e
		}
		fi, serr := d.Info()
		if serr != nil {
			return serr
		}
		if fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			rel, _ := filepath.Rel(embeddedAssetsRoot, p)
			onDisk = append(onDisk, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(onDisk)
	var listed []string
	for p := range embeddedExecutables {
		listed = append(listed, p)
	}
	sort.Strings(listed)
	if len(onDisk) == 0 {
		t.Fatal("no executable files under assets — the tree lost its mode bits")
	}
	if len(onDisk) != len(listed) {
		t.Fatalf("manifest lists %d executables, assets tree has %d — run `go generate ./...`", len(listed), len(onDisk))
	}
	for i := range onDisk {
		if onDisk[i] != listed[i] {
			t.Fatalf("manifest/tree mismatch at %q vs %q — run `go generate ./...`", listed[i], onDisk[i])
		}
	}
	for _, p := range listed {
		if _, err := fs.Stat(embeddedFS, p); err != nil {
			t.Errorf("manifest entry %q is not in the embedded bundle", p)
		}
	}
}

// materializeEmbedded must hand kati a runnable country_conf_gen.sh and a plain data file next to it.
func TestMaterializeEmbeddedRestoresExecBit(t *testing.T) {
	const rel = "device/google/tangorpro/uwb"
	if !hasEmbeddedPath(rel) {
		t.Skip("tangorpro uwb not embedded")
	}
	out := t.TempDir()
	if _, err := materializeEmbedded(rel, out); err != nil {
		t.Fatal(err)
	}
	check := func(name string, wantExec bool) {
		t.Helper()
		fi, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel), name))
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode()&0o111 != 0; got != wantExec {
			t.Errorf("%s: executable=%v, want %v (mode %v)", name, got, wantExec, fi.Mode())
		}
	}
	check("country_conf_gen.sh", true)
	check("uwb_country.conf", false)
}

func TestCopyFilePreservesMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "sub", "script.sh")
	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Fatalf("copyFile dropped the executable bit: %v", fi.Mode())
	}
}
