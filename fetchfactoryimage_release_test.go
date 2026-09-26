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
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// Google names every factory zip <device>-<build>-factory-<first 8 hex of its SHA-256>.zip, so a
// manifest row whose URL, build and checksum were copied from different table rows cannot pass.
func TestFactoryImageManifestChecksumsMatchFileNames(t *testing.T) {
	hex64 := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, e := range factoryImageManifest {
		if !hex64.MatchString(e.SHA256) {
			t.Errorf("%s %s: SHA-256 %q is not 64 lowercase hex digits", e.Device, e.Build, e.SHA256)
			continue
		}
		want := e.Device + "-" + strings.ToLower(e.Build) + "-factory-" + e.SHA256[:8] + ".zip"
		if got := path.Base(e.URL); got != want {
			t.Errorf("%s %s: URL file %q, want %q", e.Device, e.Build, got, want)
		}
		if !strings.HasPrefix(e.URL, "https://dl.google.com/dl/android/aosp/") {
			t.Errorf("%s %s: URL %q is not on dl.google.com", e.Device, e.Build, e.URL)
		}
	}
}

func TestFactoryImageManifestOneEntryPerRelease(t *testing.T) {
	seen := map[string]string{}
	for _, e := range factoryImageManifest {
		k := e.Device + "/" + factoryBuildRelease(e.Build)
		if prev, dup := seen[k]; dup {
			t.Errorf("%s: two builds for one release (%s, %s)", k, prev, e.Build)
		}
		seen[k] = e.Build
	}
}

func TestLookupFactoryImageRelease(t *testing.T) {
	cp1a := map[string]string{
		"lynx": "CP1A.260505.005", "panther": "CP1A.260405.005", "cheetah": "CP1A.260405.005",
		"tangorpro": "CP1A.260505.005", "akita": "CP1A.260505.005", "oriole": "CP1A.260405.005",
	}
	for dev, build := range cp1a {
		for _, rel := range []string{"cp1a", "CP1A"} {
			e, err := lookupFactoryImageRelease(dev, rel)
			if err != nil || e.Build != build {
				t.Errorf("%s -release %s: got %q, %v; want %s", dev, rel, e.Build, err, build)
			}
		}
		// No -release keeps the original behaviour: the device's first (android-17 cp2a) entry.
		if e, err := lookupFactoryImageRelease(dev, ""); err != nil || factoryBuildRelease(e.Build) != "cp2a" {
			t.Errorf("%s with no -release: got %q, %v; want its cp2a entry", dev, e.Build, err)
		}
	}
	if _, err := lookupFactoryImageRelease("lynx", "bp4a"); err == nil || !strings.Contains(err.Error(), "CP1A.260505.005") {
		t.Errorf("a release the manifest lacks must fail and list what it has: %v", err)
	}
	if _, err := lookupFactoryImageRelease("not-a-real-device", "cp1a"); err == nil {
		t.Error("an unknown device must fail")
	}
	if got := strings.Count(knownFactoryImageDevices(), "lynx"); got != 1 {
		t.Errorf("knownFactoryImageDevices lists lynx %d times", got)
	}
}

// rangeServer serves body with HTTP Range support and counts requests.
func rangeServer(t *testing.T, body []byte, hits *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		start := 0
		if rg := r.Header.Get("Range"); rg != "" {
			n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
			if err != nil || n >= len(body) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			start = n
			w.Header().Set("Content-Length", strconv.Itoa(len(body)-start))
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(body[start:])
	}))
}

func TestEnsureDownloaded(t *testing.T) {
	body := []byte(strings.Repeat("factory image bytes ", 500))
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])
	var hits int32
	srv := rangeServer(t, body, &hits)
	defer srv.Close()
	dir := t.TempDir()

	// A complete, verified copy is reused without a request (a Range past its end would get 416).
	complete := filepath.Join(dir, "complete.zip")
	if err := os.WriteFile(complete, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if reused, err := ensureDownloaded(srv.URL, want, complete); err != nil || !reused || hits != 0 {
		t.Errorf("complete copy: reused=%v err=%v requests=%d; want reused, no request", reused, err, hits)
	}
	// A partial copy is resumed and verified.
	partial := filepath.Join(dir, "partial.zip")
	if err := os.WriteFile(partial, body[:1234], 0o644); err != nil {
		t.Fatal(err)
	}
	if reused, err := ensureDownloaded(srv.URL, want, partial); err != nil || reused {
		t.Errorf("partial copy: reused=%v err=%v", reused, err)
	}
	if b, _ := os.ReadFile(partial); string(b) != string(body) {
		t.Errorf("partial copy was not completed: %d bytes", len(b))
	}
	// No copy: downloaded and verified.
	if reused, err := ensureDownloaded(srv.URL, want, filepath.Join(dir, "fresh.zip")); err != nil || reused {
		t.Errorf("fresh download: reused=%v err=%v", reused, err)
	}
	// A download whose bytes don't match the manifest checksum is an error.
	if _, err := ensureDownloaded(srv.URL, strings.Repeat("0", 64), filepath.Join(dir, "wrong.zip")); err == nil {
		t.Error("a checksum mismatch must fail")
	}
}
