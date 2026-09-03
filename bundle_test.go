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
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// The embedded manifest describes exactly the builtin bundle: same file set, and the content
// hashes hold (a sample is hashed; every path is stat'ed).
func TestBundleManifestMatchesBuiltin(t *testing.T) {
	if !bundleAvailable() {
		t.Skip("built with -tags nobundle")
	}
	if len(bundleEntries) < 1000 {
		t.Fatalf("manifest has %d entries — regenerate with go generate", len(bundleEntries))
	}
	seen := map[string]bool{}
	for _, e := range bundleEntries {
		seen[e.Path] = true
		if _, err := fs.Stat(embeddedFS, e.Path); err != nil {
			t.Fatalf("manifest names %s, not in the builtin bundle", e.Path)
		}
	}
	n := 0
	_ = fs.WalkDir(embeddedFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
			if !seen[p] {
				t.Errorf("builtin bundle has %s, manifest does not — run go generate", p)
			}
		}
		return nil
	})
	if n != len(bundleEntries) {
		t.Errorf("builtin bundle has %d files, manifest %d", n, len(bundleEntries))
	}
	sample := bundleEntries[:100]
	if _, err := verifyBundle(sample, embeddedFS); err != nil {
		t.Fatal(err)
	}
}

func syntheticBundle() (fstest.MapFS, []bundleEntry) {
	m := fstest.MapFS{
		"device/google/x/Android.bp": {Data: []byte("cc_library { name: \"x\" }\n")},
		"device/google/x/run.sh":     {Data: []byte("#!/bin/sh\n")},
		"hardware/google/y/y.c":      {Data: []byte("int y;\n")},
	}
	var entries []bundleEntry
	for p, f := range m {
		h := sha256.Sum256(f.Data)
		entries = append(entries, bundleEntry{Path: p, Size: int64(len(f.Data)), SHA256: hex.EncodeToString(h[:])})
	}
	return m, entries
}

func TestBundleExportFetchVerifyRoundtrip(t *testing.T) {
	src, entries := syntheticBundle()
	var buf bytes.Buffer
	if err := exportBundle(entries, src, &buf); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(buf.Bytes()) }))
	defer srv.Close()
	cache := t.TempDir()
	dir, err := fetchBundle(entries, "abc123", srv.URL+"/b.tar.gz", cache)
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(cache, "abc123") || !fileExists(filepath.Join(dir, stampName("abc123"))) {
		t.Fatalf("unexpected dir %s / missing stamp", dir)
	}
	if _, err := verifyBundle(entries, os.DirFS(dir)); err != nil {
		t.Fatal(err)
	}
	// second fetch is served from the verified cache without touching the network
	srv.Close()
	if d2, err := fetchBundle(entries, "abc123", "http://127.0.0.1:1/gone", cache); err != nil || d2 != dir {
		t.Fatalf("cache hit expected, got %v %v", d2, err)
	}
	// tamper → verify fails, and a fresh fetch of a mismatching archive is refused and removed
	if err := os.WriteFile(filepath.Join(dir, "hardware/google/y/y.c"), []byte("int z;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyBundle(entries, os.DirFS(dir)); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("tampered bundle verified: %v", err)
	}
	bad := entries
	bad[0].SHA256 = strings.Repeat("0", 64)
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(buf.Bytes()) }))
	defer srv2.Close()
	if _, err := fetchBundle(bad, "wrong", srv2.URL, cache); err == nil {
		t.Fatal("archive not matching the manifest was accepted")
	}
	if fileExists(filepath.Join(cache, "wrong")) {
		t.Fatal("rejected archive left on disk")
	}
}

func TestParseBundleManifest(t *testing.T) {
	got := parseBundleManifest("# c\n" + strings.Repeat("a", 64) + " 12 device/google/x/f g\n\nbad line\n")
	if len(got) != 1 || got[0].Path != "device/google/x/f g" || got[0].Size != 12 {
		t.Fatalf("%+v", got)
	}
}
