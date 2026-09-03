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

# Device Support

## What is in the bundle

Every device family the cp2a release names that has an AOSP tree: raviole, bluejay, pantah, lynx,
tangorpro, felix, shusky, akita, caimito, comet and tegu, with their SoC directories gs101, gs201,
zuma and zumapro. That is 16 devices, from Pixel 6 to Pixel 9a.

Four devices in cp2a have no AOSP tree in any tag and are out of reach.

Provenance is recorded per directory: everything comes from android-15.0.0_r36 except tegu, whose
tree exists only under r31. Cross-tag differences are reconciled from a manifest rather than by
special cases in code.

## What "supported" means, per level

- **Analysis gate green** — all 16 devices. The build graph is coherent.
- **Full image built and measured** — 9 devices, covering all four SoC generations.
- **Booted on hardware** — one device.

Per-device status with run identifiers lives in `CURRENT_STATE.md` in the repository.

## Adding a device that is already in the bundle

Nothing to add. Fetch its factory image and seed it. The seven devices without full images were
skipped for lack of test hardware, not for any technical reason.

## Adding a device that is not

You need its device tree from some AOSP tag. Add the directory to the bundle, record its
provenance and regenerate the manifests with `go generate ./...`, then add its factory image to
the download manifest with a verified checksum.

Expect the first full build of a **new SoC generation** to surface one or two toolkit defects. It
has happened for every new generation so far, always at build time, and the analysis gate cannot
see them. Budget an hour of machine time and read [[Troubleshooting]] first.

## Devices that are not Pixels

Not supported. The whole vendor pipeline is built around Google's self-extractor mechanism and
the Tensor SoC families. Nothing here is secretly generic.
