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
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestExtractZipAndFindInner mirrors a real factory zip's nested structure: the outer zip
// contains a <device>-<build>/ dir with a bootloader image + an inner image-*.zip, and the inner
// zip contains the flat partition images. Both extraction passes + the inner-zip finder are
// exercised together, matching cmdFetchFactoryImage's actual sequence.
func TestExtractZipAndFindInner(t *testing.T) {
	dir := t.TempDir()
	innerZip := filepath.Join(dir, "staging", "image-testdevice-build1.zip")
	writeTestZip(t, innerZip, map[string]string{
		"boot.img":   "fake boot",
		"vendor.img": "fake vendor",
	})
	outerZip := filepath.Join(dir, "outer.zip")
	innerBytes, err := os.ReadFile(innerZip)
	if err != nil {
		t.Fatal(err)
	}
	writeTestZip(t, outerZip, map[string]string{
		"testdevice-build1/bootloader-testdevice-hash.img": "fake bootloader",
		"testdevice-build1/image-testdevice-build1.zip":    string(innerBytes),
		"testdevice-build1/flash-all.sh":                   "#!/bin/bash\necho flash",
	})

	destDir := filepath.Join(dir, "out")
	if err := extractZip(outerZip, destDir); err != nil {
		t.Fatalf("extract outer zip: %v", err)
	}
	foundInner, err := findInnerImageZip(destDir)
	if err != nil {
		t.Fatalf("findInnerImageZip: %v", err)
	}
	if filepath.Base(foundInner) != "image-testdevice-build1.zip" {
		t.Fatalf("found wrong inner zip: %s", foundInner)
	}
	if err := extractZip(foundInner, destDir); err != nil {
		t.Fatalf("extract inner zip: %v", err)
	}
	for _, want := range []string{"boot.img", "vendor.img", "testdevice-build1/bootloader-testdevice-hash.img"} {
		if _, err := os.Stat(filepath.Join(destDir, want)); err != nil {
			t.Errorf("expected %s to exist after extraction: %v", want, err)
		}
	}
}

// TestExtractZipRejectsZipSlip ensures a malicious entry path (../escape.txt) is rejected rather
// than written outside destDir.
func TestExtractZipRejectsZipSlip(t *testing.T) {
	dir := t.TempDir()
	evilZip := filepath.Join(dir, "evil.zip")
	f, err := os.Create(evilZip)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../escape.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("pwned")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	destDir := filepath.Join(dir, "dest")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractZip(evilZip, destDir); err == nil {
		t.Fatal("expected extractZip to reject a zip-slip entry, got nil error")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); !os.IsNotExist(err) {
		t.Fatal("zip-slip entry was written outside destDir")
	}
}

func TestVerifySHA256(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(p, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	// sha256("hello world")
	const want = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if err := verifySHA256(p, want); err != nil {
		t.Fatalf("expected checksum to match: %v", err)
	}
	if err := verifySHA256(p, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Fatal("expected checksum mismatch to error")
	}
}

func TestLookupFactoryImage(t *testing.T) {
	for _, device := range []string{"panther", "cheetah", "lynx", "tangorpro"} {
		e, ok := lookupFactoryImage(device)
		if !ok {
			t.Errorf("expected %s to be in the manifest", device)
			continue
		}
		if e.URL == "" || e.SHA256 == "" || len(e.SHA256) != 64 {
			t.Errorf("%s: manifest entry looks malformed: %+v", device, e)
		}
	}
	if _, ok := lookupFactoryImage("not-a-real-device"); ok {
		t.Fatal("expected unknown device to not be found")
	}
}
