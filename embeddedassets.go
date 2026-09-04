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

//go:build !nobundle

package main

import (
	"embed"
	"io/fs"
)

// embeddedassets.go — the DEFAULT build embeds the bundle content itself, so "go build and run"
// stays true with no download at all (~660MB in the binary). `go build -tags nobundle` leaves the
// content out (embeddedassets_nobundle.go) and the run-time resolver in bundle.go supplies it
// from a directory, the cache or a URL — always verified against the embedded manifest.
//
// What the bundle is: a curated subset of android-15.0.0_r36's device/hardware trees. Deliberately
// EXCLUDES the <family>-kernels/ prebuilt-kernel directories: they're both the dominant size cost
// (~2GB of the ~2.2GB relevant subset) AND the wrong kernel build for a newer target release
// anyway (the target release names its own build through RELEASE_KERNEL_<DEVICE>_DIR — cp2a:
// 6.1/26Q2-15260412 — which AOSP never published) — the matching kernel directory is assembled
// from the factory image (see kernelprebuilt.go), not copied from an older AOSP tree.
// Covers every device family android-17's cp2a release names that has an AOSP tree — device/google/
// {raviole,bluejay,pantah,lynx,tangorpro,felix,shusky,akita,caimito,comet,tegu}(-sepolicy), their SoC
// dirs {gs101,gs201,zuma,zumapro}(-sepolicy), gs-common — and hardware/google/{gchips,graphics,pixel,
// pixel-sepolicy} (~660MB). Provenance per directory is in assets/aosp15_device.sources: everything is
// android-15.0.0_r36 except tegu (Pixel 9a), whose tree exists only under the android-15.0.0_r31 tag.
// `all:` because AOSP trees carry dotfiles (.clang-format) and at least one meaningful one (a
// frozen-AIDL-API .hash) that the default go:embed pattern silently drops.
//
//go:embed all:assets/aosp15_device
var embeddedAssets embed.FS

const embeddedAssetsRoot = "assets/aosp15_device"

// builtinBundle returns the bundle compiled into this binary.
func builtinBundle() fs.FS {
	sub, err := fs.Sub(embeddedAssets, embeddedAssetsRoot)
	if err != nil {
		panic("embeddedassets: bad embed root: " + err.Error())
	}
	return sub
}
