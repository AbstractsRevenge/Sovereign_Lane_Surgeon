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

# Changelog

This file records released and material user-facing changes. The live device/lane evidence and
active limitations remain in [CURRENT_STATE.md](CURRENT_STATE.md).

## Unreleased

### Added

- Added release-specific lane lunches through `-release`, including Android 16 r4 BP4A products.
- Added path-only immediate-parent suffixing through
  `-rename -suffix-parent-dirs <suffix>`. Descendant paths and module names remain unchanged.
- Added complete source-lane device inheritance for `create -from`, preserving curated packages,
  overlays, properties, signing identity, hardware wiring, and route manifests.

### Changed

- Parent-suffix lanes now use keep-name module routing while retaining distinct physical app and
  package parent paths.
- Root and parent visibility canonicalization now covers derived lane namespaces and visibility
  pseudo-targets.
- Parent path repair now covers rooted references, relative Blueprint paths, and relative overlay
  symlinks.

### Fixed

- Fixed first-lane finder generation when no sibling suffix exists.
- Fixed adding a second lane to a first lane whose cross-lane rule contains only `return false`.
- Fixed stock isolation after Holo routing and derived-lane routing are both installed.
- Restored authored-file license coverage in the test suite.

### Validated

- Generated Android 15 Holo2 from the proven Holo lane with 95 immediate parent renames.
- Passed cheetah `m nothing` for Holo2, Holo, and stock back-to-back with isolated graphs.

## 1.0.0 — 2026-09-04

### Added

- Published the factory-image-to-booting-device milestone release.
- Supported all 16 CP2A devices with an available AOSP source tree at the analysis gate.
- Produced nine preflight-valid full images across gs101, gs201, zuma, and zumapro.
- Booted Surgeon-generated Android 17 images on cheetah, panther, and lynx.

## Earlier history

The v0.4.0 lane and device-revival development history predates this changelog. Its detailed
evidence remains in [CURRENT_STATE.md](CURRENT_STATE.md), [DESIGN.md](DESIGN.md), and
[LANES.md](LANES.md).
