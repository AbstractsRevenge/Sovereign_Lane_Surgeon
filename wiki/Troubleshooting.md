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

# Troubleshooting

Every entry is a failure that actually happened, with what it turned out to mean. Symptoms in a
build this large rarely point at their cause.

## The build succeeds but the phone does not boot

**Black screen, or a reboot to the bootloader around 65 seconds, with no log.**
The vendor blobs require a bootloader and baseband the phone does not have. The build's
`android-info.txt` carries the requirement; `preflight` checks it. Flash the firmware first.

**"Preparing For Ramdump", then nothing.**
The vbmeta image was modified in flight. Never flash it with verification-disabling flags: doing
so breaks the signature, and the bootloader then hands init no verified-boot data at all. Flash it
exactly as the build signed it.

**First boot dies on unformatted partitions.**
In bootloader mode, the wipe flag only erases. Format userdata and metadata explicitly. The
generated flash script already does.

## The super image is missing a partition

`lpdump` shows no vendor partition. The cause is almost never the build system. Check whether the
vendor board configuration actually loaded: the file must sit exactly where the device tree
includes it from, which is a subdirectory, not the vendor root. `verify-seed` reports a vendor
makefile that nothing includes, which is precisely this symptom.

## A header is not found, deep in a build

```
fatal error: 'displaycolor/displaycolor_gs101.h' file not found
```

AOSP uses symlinks as compatibility headers, and `go:embed` drops symlinks entirely. If a bundle
symlink did not reach the tree, the include resolves nowhere. `verify-seed` checks every symlink
the bundle records; `bundle audit` proves the bundle has them in the first place.

## A policy statement is rejected

```
neverallow check failed ... (allow X Y:binder ...)
```

A binder call whose target is a service **name** rather than a process is rejected. Do not hand-edit
the policy: run `compat-propose` against the failed run and it will read the compiler's own report,
which names both source files and lines, and write the manifest row for you.

```bash
./sovereign-lane-surgeon compat-propose -report <run-dir> -out <aosp-root> -write-to <surgeon-src>
```

Then look for the same pattern elsewhere before rebuilding. Fixing one device's instance and
leaving its three siblings costs three more failed builds.

## Compilation fails on a warning

A newer toolchain than the code was written against. The compatibility pass relaxes this where the
target's own configuration says the project is not exempt, using the recorded toolchain versions
of both sides.

## The whole machine falls over

Two full builds plus an analysis gate exhausted 62 GB of RAM and took the editor down, losing four
hours of work. One build at a time. Run long builds from a detached process so no session timeout
kills them.

## Reading a failure at all

Every build goes through AOSP Build Capture, whose run directory is named for the verdict. Use
`audit` to classify failures against the blocker taxonomy, and `verify` to get the authoritative
pass or fail rather than trusting an exit code.
