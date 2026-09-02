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
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// embeddedassets.go — bundles a curated subset of android-15.0.0_r36's device/hardware trees
// directly into the sovereign-lane-surgeon binary, so device revival (and lane device scaffolding,
// via writeDeviceProducts' pre-seed step) need NEITHER a live AOSP15 checkout NOR a `repo sync` to
// run — "go build and run" stays true even for the device-content-heavy paths, and neither
// devs nor users need the AOSP15 tree (or the whole repo) on disk. Deliberately EXCLUDES the
// <family>-kernels/ prebuilt-kernel directories: they're both the dominant size cost (~2GB of the
// ~2.2GB relevant subset) AND the wrong kernel build for a newer target release anyway (the target
// release names its own build through RELEASE_KERNEL_<DEVICE>_DIR — cp2a: 6.1/26Q2-15260412 — which
// AOSP never published) — the matching kernel directory is assembled from the factory image
// (see kernelprebuilt.go), not copied from an older AOSP tree.
// Covers every device family android-17's cp2a release names that has an AOSP tree — device/google/
// {raviole,bluejay,pantah,lynx,tangorpro,felix,shusky,akita,caimito,comet,tegu}(-sepolicy), their SoC
// dirs {gs101,gs201,zuma,zumapro}(-sepolicy), gs-common — and hardware/google/{gchips,graphics,pixel,
// pixel-sepolicy} (~660MB). Provenance per directory is in assets/aosp15_device.sources: everything is
// android-15.0.0_r36 except tegu (Pixel 9a), whose tree exists only under the android-15.0.0_r31 tag. `all:` because AOSP trees carry
// dotfiles (.clang-format) and at least one meaningful one (a frozen-AIDL-API .hash) that the
// default go:embed pattern silently drops.
//
//go:embed all:assets/aosp15_device
var embeddedAssets embed.FS

const embeddedAssetsRoot = "assets/aosp15_device"

// embed.FS carries file contents only: every embedded file stats as 0444, so the executable bit
// AOSP's scripts rely on (kati runs device/google/tangorpro/uwb/country_conf_gen.sh via $(shell)
// and errors "Permission denied" at 0644) has to travel separately. cmd/execmanifest regenerates
// this list from the on-disk assets tree; materializeEmbedded writes listed paths as 0755.
//
//go:generate go run ./cmd/execmanifest
//go:embed assets/aosp15_device.exec
var embeddedExecManifest string

// embeddedExecutables is the manifest as a set of bundle-relative, forward-slashed paths.
var embeddedExecutables = func() map[string]bool {
	set := map[string]bool{}
	for _, line := range strings.Split(embeddedExecManifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		set[line] = true
	}
	return set
}()

// Provenance of every top-level bundle directory: one line per directory, "<path> <AOSP tag>".
// Everything is android-15.0.0_r36 except tegu (Pixel 9a), whose tree exists only under
// android-15.0.0_r31; cross-tag reconciliations live in assets/reconcile (reconcile.go).
//
//go:embed assets/aosp15_device.sources
var embeddedSourcesTable string

// bundleSourceTag returns the AOSP tag a bundle directory (e.g. "device/google/tegu") was taken
// from, or "" when the directory is not listed.
func bundleSourceTag(dir string) string {
	dir = strings.Trim(dir, "/")
	for _, line := range strings.Split(embeddedSourcesTable, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && !strings.HasPrefix(f[0], "#") && strings.Trim(f[0], "/") == dir {
			return f[1]
		}
	}
	return ""
}

// embeddedFileMode is the on-disk mode materializeEmbedded gives bundle path p.
func embeddedFileMode(p string) os.FileMode {
	if embeddedExecutables[p] {
		return 0o755
	}
	return 0o644
}

// embeddedFS is the embedded bundle rooted at embeddedAssetsRoot, so callers address paths the
// same way they would against a real AOSP tree ("device/google/lynx", not the assets/... prefix).
var embeddedFS = func() fs.FS {
	sub, err := fs.Sub(embeddedAssets, embeddedAssetsRoot)
	if err != nil {
		panic("embeddedassets: bad embed root: " + err.Error())
	}
	return sub
}()

// hasEmbeddedPath reports whether relPath (fs.FS form, forward-slashed — e.g.
// "device/google/lynx") exists in the embedded bundle.
func hasEmbeddedPath(relPath string) bool {
	_, err := fs.Stat(embeddedFS, relPath)
	return err == nil
}

// materializeEmbedded extracts embeddedFS's relPath onto outRoot/relPath on real disk. No-clobber
// (an existing target file always wins), matching mirrorStockTree/copyDeviceFamilyTree's safety
// property — safe to call speculatively before either of them runs.
func materializeEmbedded(relPath, outRoot string) (copied int, err error) {
	if _, statErr := fs.Stat(embeddedFS, relPath); statErr != nil {
		return 0, fmt.Errorf("%s not present in the embedded asset bundle", relPath)
	}
	dst := filepath.Join(outRoot, filepath.FromSlash(relPath))
	walkErr := fs.WalkDir(embeddedFS, relPath, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, rerr := filepath.Rel(filepath.FromSlash(relPath), filepath.FromSlash(p))
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if _, statErr := os.Stat(target); statErr == nil || neutralizedByBak(target) {
			return nil // no-clobber
		}
		if mirrorSkipsMakefile(outRoot, p, embeddedFS) {
			return nil // the target denylists it (overlay-replaced or an empty wrapper) — never lands
		}
		b, rerr := fs.ReadFile(embeddedFS, p)
		if rerr != nil {
			return rerr
		}
		if merr := os.MkdirAll(filepath.Dir(target), 0o755); merr != nil {
			return merr
		}
		if werr := os.WriteFile(target, b, embeddedFileMode(p)); werr != nil {
			return werr
		}
		copied++
		return nil
	})
	return copied, walkErr
}
