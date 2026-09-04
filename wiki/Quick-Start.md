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

# Quick Start

From a factory image to a booting phone. This is the sequence that was actually run on cheetah,
not an idealised one.

Assumes a synced android-17 tree and its dependencies; if you have neither, start at [[AOSP Setup]].

## 0. Build the tool

```bash
git clone git@github.com:AbstractsRevenge/Sovereign_Lane_Surgeon.git
cd sovereign_lane_surgeon
go build -o sovereign-lane-surgeon .
```

Or skip the build: the [v0.4.0 release](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/releases/tag/v0.4.0)
carries the full binary, a 15 MB slim binary, and the bundle archive the slim one fetches.

The full binary embeds the device trees, so it is large (about 490 MB). For CI or distribution,
use the slim binary and supply the trees at run time — see [[Bundle Distribution]].

## 1. Fetch the factory image

```bash
./sovereign-lane-surgeon fetch-factory-image -device cheetah -out /path/to/factory-images
```

It prints Google's terms and refuses to download until you accept. Downloads resume, and the
archive is checked against a hand-verified SHA-256.

## 2. Seed the device

```bash
./sovereign-lane-surgeon create -stock -devices cheetah -release cp2a \
    -factory-images-root /path/to/factory-images -out /path/to/android-17.0.0_r1
```

About three seconds. This mirrors the device family and everything it references, assembles the
kernel directory from the image, wires the vendor blobs, applies the compatibility operations the
target tree is probed to need, and then **verifies its own output**. Read that verdict; a FAIL
there costs seconds, while the same defect costs 25 to 46 minutes if you let it reach a build.

## 3. Build

One build at a time. Two concurrent builds plus an analysis gate exhausted a 62 GB laptop.

```bash
# analysis gate first, about four minutes — catches graph errors before you spend an hour
m nothing

# full image
m -j20 droid superimage
```

## 4. Measure before flashing

```bash
./sovereign-lane-surgeon preflight -device cheetah -out /path/to/android-17.0.0_r1 \
    -factory-images-root /path/to/factory-images
```

Compares the built images against the factory image: kernel release, super partition coverage and
fit, vbmeta coverage, firmware requirements, vendor glue. Exit 1 on any failure. See
[[Verification]] for what each check can and cannot see.

## 5. Generate the flash script

```bash
./sovereign-lane-surgeon assemble-super -device cheetah -out /path/to/android-17.0.0_r1
```

Writes `flash_<device>.sh` next to the images. When the vendor board configuration reached the
build, the build's own super image is already complete and the script simply uses it.

## 6. Flash

Phone unlocked and in fastboot. **Read the script first.** It flashes the boot chain, vbmeta
exactly as signed, the super image, then formats userdata and metadata.

```bash
bash out-aosp17/cheetah/eng/target/product/cheetah/flash_cheetah.sh
```

If the phone's bootloader or baseband are older than the vendor blobs require, flash those first.
The script prints the required versions and the two commands; it never flashes firmware itself.

## 7. Watch it boot

```bash
PATH=out-aosp17/cheetah/eng/host/linux-x86/bin:$PATH \
  aosp_runtime_log_capture -device <serial> -lane aosp17_cheetah -label first-boot -pull-tombstones
```

Measured on cheetah: adb at 21 seconds, boot completed at 48, SELinux enforcing, data encrypted,
vendor mounted through dm-verity.
