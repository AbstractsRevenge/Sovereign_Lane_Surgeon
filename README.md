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

[![CI](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/actions/workflows/ci.yml/badge.svg)](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/actions/workflows/ci.yml)

Takes a Google Pixel factory image and produces a **buildable AOSP source tree** for that phone,
then an image that boots on it, on a release whose AOSP source does not carry the phone's device
tree. Go, no external AOSP 15 checkout, no hand edits.

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
| The image boots a real phone | **Measured on one device**, cheetah (Pixel 7 Pro), twice, 48 s to lock screen. Panther and lynx boot as well, reported by the maintainer; not yet captured by the toolkit |

The per-device table is on the [wiki's Home page](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/wiki);
run identifiers are in [CURRENT_STATE.md](CURRENT_STATE.md).

## Quick start

```bash
go build -o sovereign-lane-surgeon .
./sovereign-lane-surgeon fetch-factory-image -device cheetah -out /path/to/factory-images
./sovereign-lane-surgeon create -stock -devices cheetah -release cp2a \
    -factory-images-root /path/to/factory-images -out /path/to/android-17.0.0_r1   # ~3 s, verifies itself
cd /path/to/android-17.0.0_r1 && source build/envsetup.sh && lunch aosp_cheetah-cp2a-eng
m nothing && m -j20 droid superimage                                                # one build at a time
./sovereign-lane-surgeon preflight -device cheetah -out . -factory-images-root /path/to/factory-images
./sovereign-lane-surgeon assemble-super -device cheetah -out .    # writes flash_cheetah.sh; read it, then run it
```

The full sequence, with what each step measured on cheetah, is the wiki's
[Quick Start](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/wiki/Quick-Start).

## Why this repository is large

`assets/aosp15_device/` is about 712 MB: the AOSP device trees of every supported family, copied
verbatim from `android-15.0.0_r36` (tegu from r31), so that reviving a device needs no AOSP 15
checkout. It is data, not code, and it is treated as such: content-addressed by an embedded
manifest, audited against its source tree by `bundle audit`, and optional in the binary.
`go build -tags nobundle` gives a 15 MB binary that fetches and verifies the bundle from a
release archive. Details in [DESIGN.md](DESIGN.md) and the wiki's
[Bundle Distribution](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/wiki/Bundle-Distribution).

## Releases

[v0.4.0](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/releases/tag/v0.4.0) carries
the full binary, the slim binary, and the bundle archive with its manifest, so the slim binary can
run with `-bundle-url` pointing at the archive.

## Documentation

- **[Wiki](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/wiki)**: quick start, how it works, verification, device support, troubleshooting, evidence. Source in [`wiki/`](wiki/).
- **[DESIGN.md](DESIGN.md)**: the reasoning behind every operation, the bundle, the flashing lessons, the verifiers, with the measurement each came from.
- **[CURRENT_STATE.md](CURRENT_STATE.md)**: the authoritative per-device, per-run record.
- **[LANES.md](LANES.md)**: the repository's original half, forking parallel lane builds of AOSP. `create` serves both; this is the only place the front door mentions it.

## License

Apache License 2.0, in [LICENSE](LICENSE); [NOTICE](NOTICE) records the upstream content
redistributed verbatim. Every authored file carries the header in
[androidbp_apache2_header.md](androidbp_apache2_header.md), enforced by `licenses_test.go`.
Proprietary vendor binaries are never in this repository: they come from a factory image you
download and accept Google's terms for.
