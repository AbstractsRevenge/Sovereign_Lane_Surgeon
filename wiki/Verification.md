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

# Verification

Three instruments, each answering a different question. They exist because every defect in this
port was invisible to the previous stage.

## `verify-seed` — is the seeded tree structurally correct?

Runs automatically at the end of every `create -stock`, in about a second. `-no-verify` opts out.

| Check | What it proves |
|---|---|
| vendor glue | Every vendor makefile a required directive names exists, **and no vendor makefile exists that nothing includes** |
| vendor blobs | Every blob the vendor makefiles and Blueprints name is where they name it |
| exec bits, symlinks | What the bundle records for a mirrored subtree survived into the tree |
| kernel dir | The directory the release names is populated |
| blueprints | Everything under the device and vendor trees still parses |

The second half of the vendor-glue check is the important one. A misplaced file **exists**, so
asking "is it there?" answers yes, and every include of it is the optional form, so asking "is
anything missing?" also answers yes. Only asking whether the file is reachable tells the truth.

All three of this port's real defects were replayed against it, and each is caught by name.

## `preflight` — is the built image worth flashing?

Compares the built images against the factory image they derive from, offline, before a phone is
involved. Kernel release, super partition coverage against the factory layout, group size fit,
vbmeta coverage, firmware requirements, vendor glue.

Expectations are derived per device. gs101 has no `init_boot`, no `vendor_kernel_boot` and no
`system_dlkm` partition, and preflight learns that from that device's own factory image rather
than assuming another device's layout.

**What it cannot see:** anything that only exists at run time. Policy denials, a mismatch that
appears when services start, module load order. It measures files, not behaviour.

## `bundle audit` — is the bundle a faithful copy of its source?

```bash
./sovereign-lane-surgeon bundle audit -source /path/to/android-15.0.0_r36
```

Walks a real AOSP tree beside the bundle and reports divergence by class: missing or extra files,
content, executable bits, symlinks and their targets, empty directories, and anything the bundle
cannot represent at all. Exclusions are declared in code, never applied silently.

Measured against r36: 9671 entries across 32 directories, zero divergence. Directories whose
source tree is not present report as **unaudited**, which is not the same as a pass.

## Tests

142 tests. `go test` runs an end-to-end seed from the embedded bundle into a temporary directory.
Setting `SLS_TEST_FACTORY_ROOT` to extracted factory images additionally runs the full vendor-blob
pipeline. Two further tests fail the suite when documentation drifts from the code, or when an
authored file is missing its licence header.
