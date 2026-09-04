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

# AOSP Setup

What a host needs before the [[Quick Start]] can run: the android-17 source tree, its build
dependencies, and the few extra tools this project uses. Written from the machine the port was
done on: Ubuntu 26.04, 32 cores, 62 GB of RAM.

## Hardware, honestly

| | Minimum that worked | Notes |
|---|---|---|
| RAM | 62 GB | **one build at a time.** Two concurrent -j16 builds plus an analysis gate exhausted it and took the editor down |
| Cores | 32 | full lanes run `m -j20`; a full image takes 1 to 3 hours |
| Disk | ~300 GB free | the synced tree is ~250 GB; each device's output directory adds ~120 GB (cheetah's measured 119 GB); factory images ~3 GB each |

## 1. Build dependencies

AOSP's own list for Ubuntu, all of which the port machine has installed:

```bash
sudo apt-get install git gnupg flex bison build-essential zip curl zlib1g-dev \
    libc6-dev-i386 x11proto-core-dev libx11-dev lib32z1-dev libgl1-mesa-dev \
    libxml2-utils xsltproc unzip fontconfig python3
```

Then what this project adds:

```bash
sudo apt-get install e2fsprogs android-sdk-libsparse-utils golang-go
```

- `e2fsprogs` provides `debugfs`, which lets vendor image extraction run **without root**.
- `android-sdk-libsparse-utils` provides `simg2img`, for the sparse partition images in a factory image.
- Go 1.21 or newer builds the toolkit; the port machine runs 1.26. Ubuntu's `golang-go` is fine, or install from go.dev.

Not needed: an `lz4` tool. The kernel assembly decompresses LZ4 in Go.

## 2. The `repo` tool

```bash
sudo apt-get install repo        # Ubuntu 22.04 and later
repo --version                   # the port machine runs v2.66
```

Or, if your distribution lacks the package:

```bash
mkdir -p ~/bin && curl https://storage.googleapis.com/git-repo-downloads/repo > ~/bin/repo && chmod a+x ~/bin/repo
```

`repo` needs a git identity. If you enabled GitHub's email privacy, use the noreply address here
too, since AOSP's tooling reads the same config:

```bash
git config --global user.name "Your Name"
git config --global user.email "you@example.com"
```

## 3. The android-17 tree

```bash
mkdir -p ~/AOSP_Workspace/android-17.0.0_r1 && cd ~/AOSP_Workspace/android-17.0.0_r1
repo init --partial-clone -u https://android.googlesource.com/platform/manifest -b android-17.0.0_r1
repo sync -c -j8
```

`--partial-clone` fetches blobs on demand and roughly halves the initial download; `-c` syncs
only the current branch. Expect the sync to take an hour or more on a fast connection. The tag
name matters: the release config the port uses, `cp2a`, and the device kernel directories it
names come from this tag's `build/release/`.

Do **not** expect Pixel phone device trees in that tree: android-17's manifest ships none, which
is the gap this toolkit exists to close. See [[Home]].

## 4. Verify the tree builds anything at all

Before involving a device, prove the host and tree are sound with a target that ships in AOSP:

```bash
source build/envsetup.sh
lunch aosp_cf_x86_64_phone-trunk_staging-eng
m nothing
```

`m nothing` runs Soong analysis and kati only, about four minutes on the port machine. If it is
green, the host is ready. If it is not, the problem is your environment, not this toolkit, and
[AOSP's build documentation](https://source.android.com/docs/setup/build/building) is the place
to look.

## 5. This toolkit

```bash
git clone git@github.com:AbstractsRevenge/Sovereign_Lane_Surgeon.git
cd Sovereign_Lane_Surgeon && go build -o sovereign-lane-surgeon .
```

Or take a binary from the [release](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/releases/tag/v0.4.0).
Then continue with the [[Quick Start]].

## Output directories

Every build in the port used its own output directory per device, so images and intermediates
never collide:

```bash
export OUT_DIR=out-aosp17/cheetah/eng
```

`preflight` and `assemble-super` look there by default (`-build-out` overrides). Each such
directory grows to roughly 120 GB for a full image; cheetah's measured 119 GB.
