<!--
Copyright 2026 Terrance Leverette (AbstractsRevenge)
Sovereign Lane Surgeon: https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon

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

[![CI](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/actions/workflows/ci.yml/badge.svg)](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/AbstractsRevenge/Sovereign_Lane_Surgeon)](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/releases/latest)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue)](LICENSE)

Takes a Google Pixel factory image and produces a **buildable AOSP source tree** for that phone,
then an image that boots on it, on a release whose AOSP source does not carry the phone's device
tree. Go, no external AOSP 15 checkout, no hand edits, and it checks its own work at every stage.

**v1.0.0**, 2026-09-04: factory image to booting phone, achieved on three devices.

## The gap it closes

These phones run android-17: Google publishes CP2A factory images for all twenty devices the
release names. What android-17's **AOSP source** does not include is their device trees. Its
manifest ships no Pixel phone tree at all. So you can flash Google's build onto a Pixel 7 Pro
today, and you cannot build android-17 from AOSP source for the same phone. This closes that gap:
revive the tree from the last release that published it, reconcile it against the target by
**probing the target tree** rather than carrying a patch list, and take the vendor blobs and kernel
from the factory image.

## What is proven

| Claim | Evidence |
|---|---|
| Seeds a green build from a factory image | All 16 cp2a devices with an AOSP tree, replayed from a wiped tree, no hand edits |
| Produces a complete, flashable image | 9 full images across gs101, gs201, zuma and zumapro; every one passes `preflight` |
| The image boots a real phone | **Three devices**: cheetah (Pixel 7 Pro), panther (Pixel 7), lynx (Pixel 7a); cheetah's boot instrumented on the [Evidence](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/wiki/Evidence) page, 48 s to lock screen |

Sixteen devices, Pixel 6 through Pixel 9a, are supported; four cp2a devices (blazer, frankel,
mustang, rango) have no AOSP tree in any tag and cannot be. The per-device table is on the
[wiki's Home page](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/wiki); run
identifiers are in [CURRENT_STATE.md](CURRENT_STATE.md).

## It checks its own work

Every defect this port hit was a seed that *looked* complete and failed 25 to 46 minutes into a
build, or at a black screen. So each stage now proves the previous one, and every check is derived
from the tree rather than a per-device list:

| Instrument | Question it answers | When |
|---|---|---|
| `verify-seed` | Is the seeded tree structurally sound: vendor glue reachable, blobs where their consumers name them, symlinks and executable bits intact, Blueprints parsing? | automatically, at the end of every `create -stock` |
| `preflight` | Do the built images match the factory image: kernel release, super coverage and fit, vbmeta coverage, firmware requirements? | before flashing, exit 1 on any failure |
| `bundle audit` | Is the embedded bundle a faithful copy of the AOSP tree it was cut from, across every property `go:embed` can lose? | after regenerating the bundle; 9671 entries, zero divergence |
| `compat-propose` | A full build failed: which manifest row would fix it? | reads the failure log and the target tree, writes the row |

All three of the port's real defects are replayed against `verify-seed` in the test suite, and each
is caught by name. Details in the wiki's
[Verification](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/wiki/Verification).

## Requirements

Linux x86_64, Go 1.21 or newer, an android-17 tree with AOSP's build dependencies, `debugfs`
(e2fsprogs) so vendor extraction runs without root, and `simg2img`. A full image wants about 62 GB
of RAM, **one build at a time**, and roughly 120 GB of output per device. Setting all of that up
from nothing is the wiki's
[AOSP Setup](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/wiki/AOSP-Setup).

## Quick start

Build it, or take a binary from the [latest release](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/releases/latest).

```bash
go build -o sovereign-lane-surgeon .
./sovereign-lane-surgeon fetch-factory-image -device cheetah -out /path/to/factory-images
./sovereign-lane-surgeon create -stock -devices cheetah -release cp2a \
    -factory-images-root /path/to/factory-images -out /path/to/android-17.0.0_r1   # ~3 s, then verify-seed
cd /path/to/android-17.0.0_r1 && source build/envsetup.sh && lunch aosp_cheetah-cp2a-eng
m nothing && m -j20 droid superimage                                                # one build at a time
./sovereign-lane-surgeon preflight -device cheetah -out . -factory-images-root /path/to/factory-images
./sovereign-lane-surgeon assemble-super -device cheetah -out .    # writes flash_cheetah.sh; read it, then run it
```

The full sequence, with what each step measured on cheetah, is the wiki's
[Quick Start](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/wiki/Quick-Start).

## Subcommands

| Device revival | |
|---|---|
| `fetch-factory-image` | download and extract a factory image from a hand-verified manifest; refuses until Google's terms are accepted |
| `create -stock` | revive a device: mirror its trees, assemble the kernel, wire the blobs, run the compatibility pass, verify |
| `verify-seed`, `preflight`, `bundle`, `compat-propose` | the instruments above |
| `assemble-super` | write the flash script; pack prebuilt partitions for a tree seeded before the vendor-glue fix |
| `extract-vendor`, `assemble-kernel` | the vendor and kernel halves of `create -stock`, standalone |
| `audit`, `verify`, `doctor` | classify a failed build against the blocker taxonomy |

The lane-forking half of the repository, `create` without `-stock`, `apply`, `uninstall`,
`requalify`, `rename-module`, `drop-dep` and `reexport`, is documented in [LANES.md](LANES.md).
`sovereign-lane-surgeon help` prints the full usage.

## Why this repository is large

`assets/aosp15_device/` is about 712 MB: the AOSP device trees of every supported family, copied
verbatim from `android-15.0.0_r36` (tegu from r31), so that reviving a device needs no AOSP 15
checkout. It is data, not code, and it is treated as such: content-addressed by an embedded
manifest, audited against its source tree by `bundle audit`, and optional in the binary.
`go build -tags nobundle` gives a 15 MB binary that fetches and verifies the bundle from a
release archive. Details in [DESIGN.md](DESIGN.md) and the wiki's
[Bundle Distribution](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/wiki/Bundle-Distribution).

## Releases

[v1.0.0](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/releases/tag/v1.0.0) carries
the full binary, the 15 MB slim binary, and the bundle archive with its manifest. The slim binary
runs with `-bundle-url` pointing at the archive, fetches it once, and verifies every file against
the manifest compiled into it. Each release is proven from the outside before it is announced: the
published slim binary seeding a device from the published archive.

## Documentation

- **[Wiki](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/wiki)**: AOSP setup, quick start, how it works, verification, bundle distribution, device support, troubleshooting, evidence. Source in [`wiki/`](wiki/).
- **[DESIGN.md](DESIGN.md)**: the reasoning behind every operation, the bundle, the flashing lessons, the verifiers, with the measurement each came from.
- **[CURRENT_STATE.md](CURRENT_STATE.md)**: the authoritative per-device, per-run record.
- **[LANES.md](LANES.md)**: the repository's original half, forking parallel lane builds of AOSP.

Adding a device already in the bundle is a factory image away; adding a new family means adding
its tree and expecting the first full build to surface a defect or two, as every new SoC
generation has. Both are in the wiki's
[Device Support](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/wiki/Device-Support).

## License

Apache License 2.0, free to use, modify and redistribute; the full text is in [LICENSE](LICENSE).
Every authored file carries `Copyright 2026 Terrance Leverette (AbstractsRevenge)` and the project line, in the form
[androidbp_apache2_header.md](androidbp_apache2_header.md) documents, enforced by
`licenses_test.go`. Redistributions must keep those notices and [NOTICE](NOTICE), which is how
credit travels with the code.

If you build something with it, a mention is appreciated. [CITATION.cff](CITATION.cff) gives the
form, and GitHub's "Cite this repository" button renders it.

The AOSP device trees under `assets/` keep the Android Open Source Project's own copyright and
notices; NOTICE lists every piece of redistributed upstream content. This is an independent
project, not an official part of Android or Google. Proprietary vendor binaries are never in this
repository: they come from a factory image you download and accept Google's terms for.
