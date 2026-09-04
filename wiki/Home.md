<!--
Copyright 2026 The Android Open Source Project
Copyright 2026 Sovereign Lane Surgeon

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
-->

# Sovereign Lane Surgeon

A self-contained Go toolkit that takes a **Google Pixel factory image** and produces a **buildable
AOSP source tree** for that phone, then an image that boots on it.

## Supported devices

Every cp2a device that has an AOSP device tree. "Proven" means what has actually been done, not
what is expected to work; the levels are cumulative.

| Device | Product | SoC | Tree from | Proven |
|---|---|---|---|---|
| cheetah | Pixel 7 Pro | gs201 | android-15.0.0_r36 | **Boots** — full image, preflight, flashed, toured |
| panther | Pixel 7 | gs201 | r36 | full image, preflight clean |
| lynx | Pixel 7a | gs201 | r36 | full image, preflight clean |
| tangorpro | Pixel Tablet | gs201 | r36 | full image, preflight clean |
| felix | Pixel Fold | gs201 | r36 | full image, preflight clean |
| oriole | Pixel 6 | gs101 | r36 | full image, preflight clean |
| husky | Pixel 8 Pro | zuma | r36 | full image, preflight clean |
| komodo | Pixel 9 Pro XL | zumapro | r36 | full image, preflight clean |
| tegu | Pixel 9a | zumapro | android-15.0.0_r31 | full image, preflight clean |
| raven | Pixel 6 Pro | gs101 | r36 | analysis gate green |
| bluejay | Pixel 6a | gs101 | r36 | analysis gate green |
| shiba | Pixel 8 | zuma | r36 | analysis gate green |
| akita | Pixel 8a | zuma | r36 | analysis gate green |
| tokay | Pixel 9 | zumapro | r36 | analysis gate green |
| caiman | Pixel 9 Pro | zumapro | r36 | analysis gate green |
| comet | Pixel 9 Pro Fold | zumapro | r36 | analysis gate green |

The seven at "analysis gate green" build a coherent graph from `create -stock` alone but have not
had a full image built, by choice: they share their trees and SoC with a device above that has,
and there is no test hardware for them. Every SoC generation cp2a ships has at least one full,
preflight-clean image.

**Not supported:** blazer, frankel, mustang and rango have CP2A factory images but no AOSP device
tree in any tag, so there is nothing to revive them from.

Per-device run identifiers live in
[CURRENT_STATE.md](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/blob/main/CURRENT_STATE.md).

## The gap it closes

These phones run android-17. Google supports them on it and publishes CP2A factory images for all
twenty devices the release names, at
[developers.google.com/android/images](https://developers.google.com/android/images). That is
where this toolkit gets its own inputs.

What android-17's **AOSP source** does not include is their **device trees**. Its manifest ships
no Pixel phone tree at all, only `atv`, `contexthub`, `cuttlefish`, `sdv` and `trout`. Sixteen of
the twenty cp2a devices had trees as recently as android-15; four (blazer, frankel, mustang,
rango) have never had one in any tag.

So you can flash Google's android-17 build onto a Pixel 7 Pro today, and you cannot build
android-17 **from AOSP source** for that same phone. Closing that gap is what this is for: revive
the device tree from the last release that published it, reconcile it against the target release
by probing the target rather than by carrying a patch list, and take the vendor blobs and the
kernel from the factory image.

On 2026-09-03 a Pixel 7 Pro booted an android-17 image seeded, built and flashed this way.

## What is actually proven

[![CI](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/actions/workflows/ci.yml/badge.svg)](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/actions/workflows/ci.yml)

Claims here are separated by evidence, not by confidence.

| Claim | Evidence |
|---|---|
| Seeds a green build from nothing but a factory image | All 16 cp2a devices with an AOSP tree, replayed from a wiped tree, no hand edits |
| Produces a complete, flashable image | 9 full images across gs101, gs201, zuma and zumapro; every one passes `preflight` |
| The image boots a real phone | **One device.** cheetah (Pixel 7 Pro), twice, 48 s to lock screen |
| Works on a device family it has never seen | Untested. Each new SoC family so far cost one or two toolkit defects, found at build time |

The gap between the second and third rows is the honest one. An image measured correct offline is
not the same as a phone that boots, and only gs201 has crossed it.

## Where to go next

- **[[Quick Start]]** — factory image to booting phone, the real command sequence
- **[[How It Works]]** — why it reads the target tree instead of carrying a patch list
- **[[Verification]]** — what `verify-seed`, `preflight` and `bundle audit` each prove
- **[[Device Support]]** — which devices, and what adding one costs
- **[[Troubleshooting]]** — every real failure this port hit, and what it meant
- **[[Evidence]]** — the artifacts behind the claims above, and continuous integration

Current state, per device and per run, lives in
[CURRENT_STATE.md](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/blob/main/CURRENT_STATE.md)
in the repository, which is the authoritative record. This wiki explains; it does not track status.

## Requirements

A Linux host, Go, and the usual AOSP build dependencies. `debugfs` from e2fsprogs lets vendor
extraction run without root. No external AOSP 15 checkout is needed: the device trees are in the
binary, or fetched and verified on demand.

Licensed under Apache 2.0. Proprietary vendor binaries are never distributed here; they come from
a factory image you download and accept Google's terms for.
