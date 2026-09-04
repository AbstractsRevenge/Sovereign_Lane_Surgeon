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

# Sovereign Lane Surgeon - Current State

**Last Updated:** 2026-09-04 03:10 UTC  
**Version:** v0.4.0  
**Status:** **Port complete for every SoC generation cp2a ships.** All 16 devices `m nothing` green from `create -stock` alone; 9 full images (gs201 ×5, gs101, zuma, zumapro ×2 incl. the r31 tegu tree), every one `preflight` FLASHABLE; **cheetah boots the Surgeon-built android-17 image** — first 2026-09-03 01:12 UTC, re-proven 07:11 UTC on the build's **own** complete super. Seven siblings deferred by decision (no test devices). Next horizon: Holo transformations.  

---


## cp2a device coverage (android-17.0.0_r1, updated 2026-09-03 11:00 UTC)

The cp2a release names 20 devices (its `RELEASE_KERNEL_<DEVICE>_DIR` flags are the authoritative
list: akita blazer bluejay caiman cheetah comet felix frankel husky komodo lynx mustang oriole
panther rango raven shiba tangorpro tegu tokay). 16 have an AOSP tree in some tag; the other four —
blazer, frankel, mustang, rango — have CP2A factory images but no tree in any tag, so they are out
of reach. **None of the 16 has a tree in android-17 itself**: that manifest ships no Pixel phone
device tree at all, only atv, contexthub, cuttlefish, sdv and trout. The devices run android-17 —
Google publishes their factory images — but AOSP does not carry the source to build it for them,
which is the gap `create -stock` closes. **Milestone 2026-09-02 13:11 UTC: all 16 devices `m nothing` green on android-17.0.0_r1 from `create -stock` alone.** **Milestone 2026-09-03 11:26 UTC: tegu closes the set — 9 full images across every SoC generation cp2a ships (gs201, gs101, zuma, zumapro), every one `preflight` FLASHABLE, from one bundle and one code path.** Komodo cleared secilc on the first attempt because the caimito neverallow rows went in before it reached them, from a scan prompted by husky's failure. The analysis gate is green for all 16 and is *blind* to what the last two failures were: a full build is what finds a dropped symlink or a neverallow violation, which is why one device per SoC family is built rather than none or all. **Milestone 2026-09-03 01:12 UTC: cheetah BOOTS the Surgeon-built android-17 image (lock screen, adb, enforcing, encrypted) — first Pixel running android-17 from this toolkit.** **Milestone 15:25 UTC: cheetah `m droid superimage` completed (run 152554Z) — the first full android-17 image for a Pixel from the Surgeon's own tree.** Status per device:

| device | family / SoC | tree source | factory image | `create -stock` | `m nothing` | full build | flashed |
|---|---|---|---|---|---|---|---|
| panther | pantah / gs201 | r36 | CP2A.260705.006 | ✅ replayed from wiped tree | ✅ 120519Z | ✅ 153638Z full_completed | ✅ boots (maintainer) |
| cheetah | pantah / gs201 | r36 | CP2A.260705.006 | ✅ | ✅ 120945Z | ✅ 152554Z full_completed (super.img); 10 attempts, each a new compat op or bundle asset: 5 libion header, 6 statsd proto, 7 sepolicy types, kernel-headers, 8 neverallows, 9 -Wno-error, 10 power HAL V6 | ✅ 01:11Z flashed with flash_cheetah.sh sequence, booted in 70 s: our eng build over the CP2A vendor blob, SELinux enforcing, /data encrypted, display/battery/wifi/adb up, orange state. Defect: main camera sensor (KRAKEN) I2C writes fail ENXIO at the hardware level → Google camera HAL restarts every 5 s (other sensor inits fine) |
| lynx | lynx / gs201 | r36 | CP2A.260705.006 | ✅ | ✅ 121328Z | ✅ 171442Z full_completed | ✅ boots (maintainer) |
| tangorpro | tangorpro / gs201 | r36 | CP2A.260705.006 | ✅ | ✅ 121701Z | ✅ 223034Z full_completed (from-scratch after the crash left corrupt intermediates) | — |
| felix | felix / gs201 | r36 | CP2A.260705.006 | ✅ | ✅ 054324Z (re-gated) | ✅ 075845Z full_completed (-j20) | preflight FLASHABLE |
| oriole | raviole / gs101 | r36 | CP2A.260705.006.A1 | ✅ (kernel from vendor_boot.img) | ✅ 053213Z (re-gated) | ✅ 072701Z full_completed | preflight FLASHABLE (gs101 layout: no init_boot/vendor_kernel_boot, no system_dlkm — derived from the factory image, not assumed) |
| raven | raviole / gs101 | r36 | CP2A.260705.006.A1 | ✅ | ✅ 123025Z | ⏳ | — |
| bluejay | bluejay / gs101 | r36 | CP2A.260705.006.A1 | ✅ | ✅ 123425Z | ⏳ | — |
| shiba | shusky / zuma | r36 | CP2A.260805.005 | ✅ | ✅ 124842Z | ⏳ | — |
| husky | shusky / zuma | r36 | CP2A.260805.005 | ✅ | ✅ 125137Z | ✅ 102510Z full_completed | preflight FLASHABLE — **first zuma full image**; took 3 attempts, both failures were real toolkit bugs (083328Z dropped symlink, 092940Z zuma neverallow) |
| akita | akita / zuma | r36 | CP2A.260805.005 | ✅ | ✅ 125430Z | ⏳ | — |
| tokay | caimito / zumapro | r36 | CP2A.260805.005.A1 | ✅ | ✅ 123822Z | ⏳ | — |
| caiman | caimito / zumapro | r36 | CP2A.260805.005.A1 | ✅ | ✅ 124121Z | ⏳ | — |
| komodo | caimito / zumapro | r36 | CP2A.260805.005.A1 | ✅ | ✅ 125721Z | ✅ 103613Z full_completed | preflight FLASHABLE — **first zumapro full image**; cleared secilc first time on the caimito neverallow rows added before it got there |
| comet | comet / zumapro | r36 | CP2A.260805.005.A1 | ✅ | ✅ 130022Z | ⏳ | — |
| tegu | tegu / zumapro | **r31** (Pixel 9a's own branch) | CP2A.260805.005 | ✅ (its include-only root Android.mk is denylisted by 17 and removed) | ✅ 060530Z (re-gated) | ✅ 112641Z full_completed | preflight FLASHABLE — the only r31 tree, built like the rest |

What the wider families taught the toolkit (all in `create -stock` now): gs101 has no
vendor_kernel_boot.img — first-stage modules and the dtb come from vendor_boot.img's `dlkm` ramdisk
fragment and the board reads `modules.load` (gs201/zuma read `vendor_kernel_boot.modules.load`;
AOSP 15 shipped both names, identical, and so does the assembler); the dtb section of the vendor
boot image is written as `<image>.dtb` for the build's dtb.img rule; zuma/zumapro device trees
carry no `-pedantic`, only the system-property idiom.

## 🎯 Project Overview

The Sovereign Lane Surgeon is a self-contained Go toolkit for creating parallel "lane" builds in AOSP without forking the entire tree, and — as of v0.4.0 — for **reviving physical Pixel devices on a newer AOSP release** from the embedded device trees plus a factory image, with no hand edits.

### Primary Goal
**Complete the end-to-end pipeline that takes a Pixel factory image and produces a working AOSP 17 build for that physical device.** — Reached 2026-09-03: cheetah boots the Surgeon-built android-17 image.

---

## ✅ What's Working

### Core Toolkit Infrastructure
| Component | Status | Details |
|-----------|--------|---------|
| Lane scaffolding (`create`) | ✅ Working | Generates device/emu products, stages soong patches; stock-seeded lanes proven to a green image (v0.4.0 branch, merged) |
| Soong patch system | ✅ Working | Preview-then-apply with snapshots |
| AST operations | ✅ Working | Blueprint/Go AST patching (no regex): rename, drop-dep, requalify, cflag drop, AIDL re-pin, defaults pinning, header_libs add |
| Uninstall/rollback | ✅ Working | Byte-identical reversal |
| Audit/classification | ✅ Working | 23-class taxonomy incl. the android-17 classes (illegal cflag, AIDL version conflict, system-props artifact path, neverallow violation, stale generated mk, kernel module rule collision) |
| Test suite | ✅ Working | run on every push by CI (badge in README); 142 tests (`go test ./...`), including an **end-to-end seed** from the embedded bundle into a temp root (and, with `SLS_TEST_FACTORY_ROOT` set, the full vendor-blob pipeline); `docs_test.go` fails the suite when README/CURRENT_STATE drift from the code |

### Device Revival (`create -stock`)
| Component | Status | Details |
|-----------|--------|---------|
| Bundle fidelity | ✅ **Proven** | embed.FS carries content only. All four properties travel as manifests from ONE generator (`.sha256`, `.exec`, `.symlinks`, `.dirs`); `bundle audit -source <tree>` compares the bundle against a real AOSP tree and found **zero divergence across 9671 entries / 32 directories** (2026-09-03) |
| Seed self-check | ✅ Working | `verify-seed`, run automatically by `create -stock`: vendor glue reachable, blobs where their consumers name them, exec bits and symlinks intact, kernel dir populated, Blueprints parse — all derived from the tree. Replays all three of this port's real defects and catches each |
| Embedded device trees | ✅ Working | every cp2a family with an AOSP tree: raviole, bluejay, pantah, lynx, tangorpro, felix, shusky, akita, caimito, comet (r36) + tegu (r31); SoC dirs gs101/gs201/zuma/zumapro; provenance in `assets/aosp15_device.sources` |
| Embedded hardware HALs | ✅ Working | gchips, graphics (with the Soong-conversion overlay for graphics/common), pixel, pixel-sepolicy; per-family kernel headers |
| Reference closure | ✅ Working | every device/google + hardware/google subtree the family references is mirrored transitively; upstream git projects left alone |
| Target-compat pass | ✅ Working | 10 operations, each gated on a probe of the target tree (see README) |
| Kernel prebuilt assembly | ✅ Working | from the factory image: boot/dtbo/Image.lz4, first-stage modules (vendor_kernel_boot or gs101's vendor_boot), dtb, lists, headers; version read from the image |
| Vendor blob wiring | ✅ Working | self-extractor mechanism + factory android-info.txt (firmware requirements) via USE_ANDROID_INFO |
| Vendor extraction | ✅ Working | vendor / vendor_dlkm / system_dlkm / system_ext via debugfs, no root; system_ext blobs wired where device-partial.mk / Android.bp.txt name them |
| Bundle distribution | ✅ Working | content-addressed: manifest always embedded, content builtin or `-tags nobundle` + `-bundle-dir`/`-bundle-url` verified against it; `bundle export` publishes |
| Compat table growth | ✅ Working | ops 5/6/8 read asset manifests; `compat-propose` derives rows from a failed build's log + target tree |
| Docs consistency | ✅ Working | `docs_test.go` pins version, subcommands, op count, device list, provenance |
| Executable bits | ✅ Working | exec manifest restores modes embed.FS drops |

### Flashing (`assemble-super`, `flash_<device>.sh`)
| Component | Status | Details |
|-----------|--------|---------|
| Complete super | ✅ Working | from the build itself once the vendor glue is where the device tree includes it (root cause found 2026-09-03); `assemble-super` packs prebuilt partitions only for a pre-fix tree |
| Preflight | ✅ Working | `preflight`: kernel release, super coverage/fit (liblp parsed in Go), vbmeta coverage vs the factory, android-info firmware lines, vendor glue — exit 1 on any FAIL |
| Flash script | ✅ Working | firmware requirements, boot chain, vbmeta as signed, full super, explicit f2fs format, reboot |
| Runtime capture | ✅ Working | Build Capture's `aosp_runtime_log_capture` with the build's host adb on PATH |

### Factory Image Fetching (`fetch-factory-image`)
| Component | Status | Details |
|-----------|--------|---------|
| Manifest | ✅ Working | 16 cp2a devices, hand-verified URLs + SHA-256 (2026-09-02) |
| Download with resume | ✅ Working | Accept-Ranges aware |
| Extraction | ✅ Working | outer zip → image-*.zip → partition images, zip-slip safe |

---

## 🔧 What We're Currently Working On

### Primary Task: android-17 port — done for the Pixel 7 family
**Status (2026-09-03):** all 16 cp2a devices `m nothing` green from `create -stock` alone; full images for panther, cheetah, lynx, tangorpro; cheetah booted and toured. Remaining full builds beyond felix/oriole deferred by decision (no test devices for those families). Next horizon (T): Holo transformations on top of the revived devices.

### Secondary Task: Documentation
- [x] README, CURRENT_STATE, port changelog current as of 2026-09-03
- [x] Known limitations and workarounds documented below

---

## 🚧 Known Limitations & Future Work

### Current Limitations

| Limitation | Impact | Workaround |
|------------|--------|------------|
| Boot proven on gs201 only | felix (gs101) and oriole (zuma) images are measured by `preflight`, not booted | a test device of each family |
| Projects the target manifest ships must never be overwritten from AOSP 15 | Duplicate/undefined modules that surface far from the cause | Restore from the project's git HEAD; only copy directories absent from the target manifest |
| Kernel prebuilt dir is not in AOSP for cp2a | Board reads `RELEASE_KERNEL_<DEVICE>_DIR` | `create -stock -release cp2a` / `assemble-kernel` builds it from the factory image (pure Go, no root) |
| `simg2img` required | Vendor image extraction fails | `sudo apt-get install android-sdk-libsparse-utils` |
| Firmware flash is manual | A vendor on older bootloader/baseband dies at ~65 s with no log | `flash_<device>.sh` prints the required versions and the two fastboot commands; the tool never flashes firmware |
| Camera on T's cheetah | provider crash-loops (KRAKEN I2C ENXIO) | hardware fault of the unit — identical on stock |
| Sudo required for mount | Only when `debugfs` is absent | Install e2fsprogs; `debugfs rdump` is tried first and needs no root |

### Future Enhancements

| Feature | Priority | Description |
|---------|----------|-------------|
| `system_ext.img` extraction | ✅ Done | debugfs, wired by consumer path (2026-09-03) |
| Full `m droid` validation | ✅ Done | cheetah booted 2026-09-03 |
| `doctor` auto-apply | 🟡 Medium | Automated fix application from audit |
| Kernel prebuilt fetch | ✅ Done | `assemble-kernel` from the factory image (v0.4.0) |
| Android 16 support | 🟡 Medium | Test/update for AOSP 16 |
| Additional devices | ✅ Done (analysis gate) | all 16 cp2a devices with an AOSP tree; full builds for zuma/zumapro families on demand |
| Docker/container support | 🟢 Low | Root-free path exists now (debugfs); containerizing is what remains |

---

## 📊 Progress Tracker

### Physical Device Revival Progress

```
cp2a on android-17.0.0_r1 — from factory image to a booting phone
├── [✅] Factory images fetched + verified (16 devices, hand-verified manifest)
├── [✅] vendor / vendor_dlkm / system_dlkm / system_ext extracted (debugfs, no root)
├── [✅] Device trees + referenced subtrees mirrored (bundle: content, exec bits, symlinks)
├── [✅] Kernel prebuilt dirs assembled from the images (6.1, 26Q2-15260412)
├── [✅] Target-compat pass, 10 operations, all probed — no hand edit
├── [✅] Vendor glue where the device tree includes it → complete super from the build itself
├── [✅] m nothing green: all 16 devices
├── [✅] m droid superimage: cheetah, panther, lynx, tangorpro, felix (gs201) · oriole (gs101) · husky (zuma)
├── [✅] m droid superimage: komodo (zumapro) 103613Z, tegu 112641Z (the only r31 tree)
├── [✅] preflight FLASHABLE on every completed image (9/9)
├── [✅] Physical boot: cheetah ×2 — 2nd on the build's own super, 48 s to lock screen
└── [✅] Tour + runtime capture (camera fault traced to this unit's hardware, not the port)

Deferred by decision (no test devices; identical trees, gates already green):
    raven, bluejay (gs101) · shiba, akita (zuma) · tokay, caiman, comet (zumapro)
```

### Code Enhancement Progress

```
v0.4.0 Enhancements
├── [✅] vendorutils.go (shared utilities)
├── [✅] extractVendorImageContents function
├── [✅] fetchfactoryimage.go integration
├── [✅] devicerevival.go integration
├── [✅] README.md update
├── [⏳] CURRENT_STATE.md (this document)
└── [⏳] System_ext.img extraction

Complete: 6/8 tasks (75%)
```

---

## 🔍 Test Results Summary

### Successful Tests
1. **Factory image fetch**: Full download → extract → vendor content extraction completed successfully on Panther
2. **Stock device revival**: No-clobber preserved manual fixes, added missing files
3. **Vendor blob extraction**: `vendor.google_devices/panther/proprietary/` populated with blob files
4. **Kernel version**: Correctly set to 6.12 in product makefiles

### Build Status
```bash
# `m nothing` (analysis gate), all 16 cp2a devices, android-17.0.0_r1, 2026-09-02: completed_bootstrap
# Re-gated 2026-09-03 on the loaded vendor board config (glue under proprietary/, system_ext blobs, LICENSE/COPYRIGHT), one build at a time: completed_bootstrap
#   cheetah 032523Z  lynx 052033Z  panther 052436Z  tangorpro 052825Z  oriole 053213Z  raven 053604Z  bluejay 053947Z  felix 054324Z
#   shiba 054709Z  husky 054952Z  akita 055230Z  tokay 055506Z  caiman 055742Z  komodo 060016Z  comet 060252Z  tegu 060530Z
#   panther 120519Z  cheetah 120945Z  lynx 121328Z  tangorpro 121701Z  felix 122559Z
#   raven 123025Z    bluejay 123425Z  tokay 123822Z  caiman 124121Z    oriole 124446Z
#   shiba 124842Z    husky 125137Z    akita 125430Z  komodo 125721Z    comet 130022Z  tegu 131105Z
# `m -j32 droid superimage` (full image): cheetah 152554Z, panther 153638Z, lynx 171442Z, tangorpro 223034Z
# boot: cheetah 2026-09-03 01:12Z — adb at 40 s, boot_completed at 70 s, Enforcing, encrypted, orange
```

---

## 🔬 What a full build finds that the gate cannot (2026-09-03)

Husky (first zuma) needed three attempts. Neither failure was a device-tree defect — both were
toolkit bugs invisible to `m nothing`, and both are fixed in the Surgeon, not patched in the tree:

| run | failed at | cause | fix |
|---|---|---|---|
| 083328Z | 46 min, `libacryl_hdr_plugin` | `go:embed` drops **symlinks** entirely (as it drops exec bits). The bundle lost 5, incl. `graphics/{zuma,zumapro}/include/displaycolor/displaycolor_gs101.h -> ../gs101/…`, which is how libacryl's `include_dirs` resolve that header | `assets/aosp15_device.symlinks` + `cmd/symlinkmanifest`; `materializeSymlink` recreates each link after the files it points at, `bundle export` carries them as tar symlink entries, a test pins the manifest to the assets tree |
| 092940Z | 25 min, `precompiled_sepolicy` | `binder_call(hal_radioext_default, gril_antenna_tuning_service)` — 17 forbids a binder call to a `service_manager_type` (a service *name*, not a domain) via `neverallow * { -domain }:binder *` | `compat-propose` learned secilc's **CIL** report (which names both source files *and* lines) and wrote the op-8 row itself; a scan of every `binder_call` target declared `service_manager_type` found 3 more in caimito, added before komodo could hit them. The `:service_manager find`/`add` rules stay |

Cost of finding them: ~70 minutes of machine time. Cost of not finding them: every zuma and
zumapro device silently unbuildable, with a green analysis gate.

---

## 📦 Published (2026-09-04)

- **Repository public** at github.com/AbstractsRevenge/Sovereign_Lane_Surgeon, history rewritten to the noreply identity before publication; CI green on the first run.
- **Release v0.4.0**: full binary (511 MB), slim binary (15 MB, `-tags nobundle`), bundle archive `sls-bundle-692dded8e614a01b.tar.gz` (304 MB) and its manifest. **Proven from the live URLs:** the published slim binary fetched the published archive, verified all 7130 files, seeded lynx into an empty root, and passed its own structural checks.
- **Front door**: README is 92 lines; the device-revival reference is DESIGN.md, the lane toolkit is LANES.md, orientation is the wiki (9 pages, three ROM screenshots, supported-devices table on Home).

## 🎯 Next Steps

### Immediate (This Session)
1. [x] Fix `connection_manager` undefined module
2. [x] Fix `libgfxstream_backend` undefined module
3. [x] Get `m nothing` to 100% completion
4. [x] Generate `build.aosp_panther.ninja.d` (now at `out-aosp17/panther/eng/soong/`)
5b. [x] Faithful green: `create -stock` derives the required subtree set by reference closure and runs the target-compat pass (illegal cflags, system props, AIDL sibling conflicts, denylisted Android.mk → Soong-conversion overlay), all probed on the target tree; replayed from a wiped tree 2026-09-02. Verdict: `m nothing` green for all four on that tree (panther 120519Z, cheetah 120945Z, lynx 121328Z, tangorpro 121701Z) (the first attempt, 120133Z, failed on the denylisted graphics/common Android.mk — that failure is what produced operation 4).
5a. [x] Executable bits: embed.FS drops modes; `assets/aosp15_device.exec` + `cmd/execmanifest` restore them (tangorpro `m nothing` was failing on `country_conf_gen.sh: Permission denied`); `copyFile` preserves perms
5. [x] Kernel prebuilts: `assemble-kernel` builds `pantah-kernels/6.1/26Q2-15260412` (and lynx/tangorpro) from the factory images; `gs201/BoardConfig-common.mk` restored to pristine; `TARGET_LINUX_KERNEL_VERSION` set to 6.1 (the kernel the image actually carries)

### Short-term (This Week)
1. [x] Run `m droid superimage` successfully — cheetah 152554Z, panther 153638Z, lynx 171442Z, tangorpro 223034Z (six new compat ops + kernel headers came out of cheetah's ten attempts)
2. [x] Boot on a physical device — cheetah, 2026-09-03 01:12 UTC (`assemble-super` + `flash_cheetah.sh`; firmware, vbmeta-as-signed and explicit f2fs format were the three flash lessons)
3. [x] Port Cheetah, Lynx, Tangorpro — and the other twelve cp2a devices at the `m nothing` gate
5. [x] **Full builds complete, one at a time:** gs201 (cheetah 062805Z, panther 071321Z, lynx 071754Z, tangorpro 072229Z, felix 075845Z), gs101 (oriole 072701Z), zuma (husky 102510Z, third attempt — two toolkit bugs), zumapro (komodo 103613Z first attempt, tegu 112641Z on the r31 tree). All 9 `preflight` FLASHABLE. Deferred (no test devices; identical trees, green gates): raven, bluejay, shiba, akita, tokay, caiman, comet.
5. [x] Cheetah camera — CLOSED as a fault of this unit, not the port (2026-09-03 02:18Z). Control test on stock (factory system, vendor and boot chain, matching firmware, captured with the same tool into `…/20260903T021809Z_boot_ok_stock-camera-black/`): 0 camera devices, 414 provider crashes, all 32 tombstones carry the identical abort `KRAKEN init error … LwisFence signal status: No such device or address`, rear and front cameras black. Our image's chain of evidence (kernel `lwis-sensor-kraken: I2C Write failed (-6)` → HAL abort loop → camera service add/remove → 0 devices → Camera2 app AIOOBE) is the same fault seen from an eng build. Boot images, module loading and bootconfig were each excluded on the way (factory boot chain over our super fails identically).

### Medium-term (This Month)
1. [x] System_ext.img extraction (2026-09-03)
2. [x] **One AOSP build at a time on this laptop** (T, 2026-09-03 ~04:00 UTC): two -j16 full builds plus paired gates exhausted memory and crashed VS Code — felix/oriole lost at ~70%, five gate runs killed (their `failed` dirs 034200Z–034324Z are interleaved-log/lock artifacts, not build errors). Gates now run strictly sequentially (detached runner); full builds one at a time.
3. [x] Vendor glue root cause fixed; all 16 `m nothing` green on the loaded vendor board config (run ids in the coverage block; lynx's first re-gate 051644Z exposed the lib/lib64 placement bug, fixed in dc1eb23)
4. [x] **cheetah rebuilt on the fixed glue and re-proven (2026-09-03 07:11 UTC)**: Build Capture run 062805Z (8 min, 603 steps — labelled `completed_noop` because siso prints no progress lines off a TTY; Build Capture now parses siso's `Build Succeeded: N steps` line), `preflight` all PASS on the build's own super.img (vendor 802M + vendor_dlkm 55M inside; vbmeta now chains vbmeta_vendor; android-info carries the firmware lines), flashed with the generated script (wipe), adb 21 s, boot_completed 48 s, enforcing, encrypted, vendor/vendor_dlkm mounted through dm-verity, baseband reported, ShannonIms/ShannonRcs/UwbVendorService installed on system_ext from the newly wired blobs. Runtime capture 071219Z_boot_ok_rebuild-complete-super; the only tombstones are the camera provider's known hardware fault.
2. [ ] Flash and tour a second family (gs101 or zuma) once its full build lands — the flash script is per device
3. [x] Merge v0.4.0 lane work onto main (683d196)
4. [x] Documentation for the android-17 port: README (operations 1–10, bundle, kernel, flashing), this file, the port changelog

---

## 📝 How to Contribute / Test

### Building and Testing the Surgeon

```bash
# Clone and build
git clone https://github.com/abstractsrevenge/sovereign_lane_surgeon
cd sovereign_lane_surgeon
go build -o sovereign-lane-surgeon .

# Test factory image fetching
./sovereign-lane-surgeon fetch-factory-image \
    -device panther \
    -out /path/to/factory-images \
    -i-accept-google-terms

# Test stock device revival
./sovereign-lane-surgeon create -stock \
    -devices panther \
    -out /path/to/aosp-17 \
    -kernel-version 6.12 \
    -hw-subtrees hardware/google/gchips,hardware/google/graphics,hardware/google/pixel \
    -factory-images-root /path/to/factory-images

# Build the device
cd /path/to/aosp-17
export BUILD_BROKEN_SRC_DIR_IS_WRITABLE=true
source build/envsetup.sh
lunch aosp_panther-cp2a-eng
m nothing -j20
```

### Reporting Issues

When reporting issues, include:
- The exact command run
- Full error output
- Contents of `vendor/google_devices/<device>/` if relevant
- AOSP version and device

---

## 🔗 Related Files

| File | Purpose |
|------|---------|
| `vendorutils.go` | Shared utilities for vendor image extraction (v0.4.0) |
| `fetchfactoryimage.go` | Factory image download + extraction + vendor content extraction |
| `devicerevival.go` | Stock device revival (mirror trees, wire blobs) |
| `vendorblobs.go` | Vendor blob wiring from factory images |
| `embeddedassets.go` | //go:embed of AOSP 15 device trees |
| `README.md` | Project overview and documentation |

---

## 📋 Known Good Configurations

### Working Build Environment
```
AOSP Version: android-17.0.0_r1
Device: Panther (Pixel 7)
Build: CP2A.260605.016
Lunch: aosp_panther-cp2a-eng
Kernel: 6.12
Environment: BUILD_BROKEN_SRC_DIR_IS_WRITABLE=true
```

### Required Tools
```
go 1.21+
simg2img (android-sdk-libsparse-utils)
sudo (for mount)
rsync (optional, fallback to cp)
```

---

## 🎉 Success Metrics

We'll consider v0.4.0 complete when:

1. [ ] `fetch-factory-image` fully extracts all vendor images (including `system_ext.img`)
2. [ ] `create -stock` produces a build that reaches `m nothing` 100%
3. [ ] `m droid` completes successfully
4. [ ] Generated images boot on physical Panther device
5. [ ] All 4 devices (Panther, Cheetah, Lynx, Tangorpro) working
6. [ ] Documentation updated with full workflow

---

**Last Updated:** September 2026  
**Next Review:** After the kernel prebuilt directory is assembled and `m droid` is attempted
