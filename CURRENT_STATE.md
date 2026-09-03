# Sovereign Lane Surgeon - Current State

**Last Updated:** 2026-09-03  
**Version:** v0.4.0  
**Status:** Physical Device Revival Phase — all 16 cp2a devices `m nothing` green; gs201 family full images built; **cheetah boots the Surgeon-built android-17 image** (2026-09-03 01:12 UTC); remaining full builds running two at a time

---


## cp2a device coverage (android-17.0.0_r1, updated 2026-09-02)

The cp2a release names 20 devices; 16 have an AOSP tree (Pixel 10 family: blazer, frankel, mustang,
rango, stallion have CP2A factory images but no tree in any tag — out of reach). **Milestone 2026-09-02 13:11 UTC: all 16 devices `m nothing` green on android-17.0.0_r1 from `create -stock` alone.** **Milestone 2026-09-03 01:12 UTC: cheetah BOOTS the Surgeon-built android-17 image (lock screen, adb, enforcing, encrypted) — first Pixel running android-17 from this toolkit.** **Milestone 15:25 UTC: cheetah `m droid superimage` completed (run 152554Z) — the first full android-17 image for a Pixel from the Surgeon's own tree.** Status per device:

| device | family / SoC | tree source | factory image | `create -stock` | `m nothing` | full build | flashed |
|---|---|---|---|---|---|---|---|
| panther | pantah / gs201 | r36 | CP2A.260705.006 | ✅ replayed from wiped tree | ✅ 120519Z | ✅ 153638Z full_completed | — |
| cheetah | pantah / gs201 | r36 | CP2A.260705.006 | ✅ | ✅ 120945Z | ✅ 152554Z full_completed (super.img); 10 attempts, each a new compat op or bundle asset: 5 libion header, 6 statsd proto, 7 sepolicy types, kernel-headers, 8 neverallows, 9 -Wno-error, 10 power HAL V6 | ✅ 01:11Z flashed with flash_cheetah.sh sequence, booted in 70 s: our eng build over the CP2A vendor blob, SELinux enforcing, /data encrypted, display/battery/wifi/adb up, orange state. Defect: main camera sensor (KRAKEN) I2C writes fail ENXIO at the hardware level → Google camera HAL restarts every 5 s (other sensor inits fine) |
| lynx | lynx / gs201 | r36 | CP2A.260705.006 | ✅ | ✅ 121328Z | ✅ 171442Z full_completed | — |
| tangorpro | tangorpro / gs201 | r36 | CP2A.260705.006 | ✅ | ✅ 121701Z | ✅ 223034Z full_completed (from-scratch after the crash left corrupt intermediates) | — |
| felix | felix / gs201 | r36 | CP2A.260705.006 | ✅ | ✅ 122559Z | ⏳ running (-j16, started 01:16Z 2026-09-03) | — |
| oriole | raviole / gs101 | r36 | CP2A.260705.006.A1 | ✅ (kernel from vendor_boot.img) | ✅ 124446Z | ⏳ running (-j16, started 01:16Z 2026-09-03) | — |
| raven | raviole / gs101 | r36 | CP2A.260705.006.A1 | ✅ | ✅ 123025Z | ⏳ | — |
| bluejay | bluejay / gs101 | r36 | CP2A.260705.006.A1 | ✅ | ✅ 123425Z | ⏳ | — |
| shiba | shusky / zuma | r36 | CP2A.260805.005 | ✅ | ✅ 124842Z | ⏳ | — |
| husky | shusky / zuma | r36 | CP2A.260805.005 | ✅ | ✅ 125137Z | ⏳ | — |
| akita | akita / zuma | r36 | CP2A.260805.005 | ✅ | ✅ 125430Z | ⏳ | — |
| tokay | caimito / zumapro | r36 | CP2A.260805.005.A1 | ✅ | ✅ 123822Z | ⏳ | — |
| caiman | caimito / zumapro | r36 | CP2A.260805.005.A1 | ✅ | ✅ 124121Z | ⏳ | — |
| komodo | caimito / zumapro | r36 | CP2A.260805.005.A1 | ✅ | ✅ 125721Z | ⏳ | — |
| comet | comet / zumapro | r36 | CP2A.260805.005.A1 | ✅ | ✅ 130022Z | ⏳ | — |
| tegu | tegu / zumapro | **r31** (Pixel 9a's own branch) | CP2A.260805.005 | ✅ (its include-only root Android.mk is denylisted by 17 and removed) | ✅ 131105Z (after the reconciliation) | ⏳ | — |

What the wider families taught the toolkit (all in `create -stock` now): gs101 has no
vendor_kernel_boot.img — first-stage modules and the dtb come from vendor_boot.img's `dlkm` ramdisk
fragment and the board reads `modules.load` (gs201/zuma read `vendor_kernel_boot.modules.load`;
AOSP 15 shipped both names, identical, and so does the assembler); the dtb section of the vendor
boot image is written as `<image>.dtb` for the build's dtb.img rule; zuma/zumapro device trees
carry no `-pedantic`, only the system-property idiom.

## 🎯 Project Overview

The Sovereign Lane Surgeon is a self-contained Go toolkit for creating parallel "lane" builds in AOSP without forking the entire tree. Currently, we are **extending it to fully automate physical device revival** for Pixel devices (Panther, Cheetah, Lynx, Tangorpro) on Android 17.

### Primary Goal
**Complete the end-to-end pipeline that takes a Pixel factory image and produces a working AOSP 17 build for that physical device.**

---

## ✅ What's Working

### Core Toolkit Infrastructure
| Component | Status | Details |
|-----------|--------|---------|
| Lane scaffolding (`create`) | ✅ Working | Generates device/emu products, stages soong patches |
| Soong patch system | ✅ Working | Preview-then-apply with snapshots |
| AST operations | ✅ Working | Blueprint/Go AST patching (no regex) |
| Uninstall/rollback | ✅ Working | Byte-identical reversal |
| Audit/classification | ✅ Working | 17-class taxonomy for build failures |
| Test suite | ✅ Working | 112 tests, all passing (`go test ./...`) |

### Device Revival (`create -stock`)
| Component | Status | Details |
|-----------|--------|---------|
| Embedded device trees | ✅ Working | ~254MB of AOSP 15 assets (pantah, lynx, tangorpro, gs101, gs201, gs-common) |
| Embedded hardware HALs | ✅ Working | gchips, graphics, pixel, pixel-sepolicy |
| Family/SoC resolution | ✅ Working | Auto-detects from tree or embedded bundle |
| Tree mirroring | ✅ Working | Verbatin copy, no-clobber, .git-skipping |
| Kernel version reconciliation | ✅ Working | Sets `TARGET_LINUX_KERNEL_VERSION` |
| Hardware subtree mirroring | ✅ Working | `-hw-subtrees` flag |
| Vendor blob wiring | ✅ Working | Parses `self-extractors_*`, copies partition images |

### Factory Image Fetching (`fetch-factory-image`)
| Component | Status | Details |
|-----------|--------|---------|
| Download with resume | ✅ Working | Accept-Ranges aware |
| SHA-256 verification | ✅ Working | Against hand-verified manifest |
| Outer ZIP extraction | ✅ Working | Zip-slip safe |
| Inner image ZIP extraction | ✅ Working | Finds and extracts `image-*.zip` |
| **Vendor image content extraction** | ✅ **NEW (v0.4.0)** | Extracts `vendor.img` and `vendor_dlkm.img` contents |

### Vendor Image Content Extraction (v0.4.0)
| Component | Status | Details |
|-----------|--------|---------|
| Sparse image detection | ✅ Working | `file` command |
| Sparse→raw conversion | ✅ Working | `simg2img` |
| Mount raw image | ✅ Working | `sudo mount -o loop` |
| Copy files | ✅ Working | `rsync` or `sudo cp -r` fallback |
| Unmount/cleanup | ✅ Working | Deferred cleanup |
| Ownership fix | ✅ Working | `sudo chown` |
| `vendor.img` → `proprietary/` | ✅ Working | All vendor blob files |
| `vendor_dlkm.img` → `dlkm/` | ✅ Working | Kernel modules |

---

## 🔧 What We're Currently Working On

### Primary Task: Android 17 Panther Port
We are using the Surgeon to port Panther (Pixel 7) to AOSP 17, validating the toolkit and identifying any gaps.

**Current Status (2026-09-02):** `aosp_panther-cp2a-eng` `m nothing` is GREEN (Build Capture run `20260902T092636Z_panther_completed_bootstrap_nothing`, 0 Soong/kati/ninja errors, Soong re-analyzed). The earlier undefined modules were not missing between releases: the hand bring-up had overwritten `hardware/google` with AOSP 15 content and renamed 80 Blueprints to `.bak`. Full evidence and the change list: `15_17_device_build_diffs/ninja_d_analysis_20260902/`.

| Issue | Status | Action |
|-------|--------|--------|
| Duplicate product definition | ✅ Fixed | Backup directories moved out of build tree |
| Release config | ✅ Fixed | Using `aosp_panther-cp2a-eng` lunch combo |
| `connection_manager` / `libgfxstream_backend` undefined | ✅ Fixed | Restored the 7 overwritten 17 git projects to HEAD (never copy a project the target manifest ships) |
| hwc3 cross-namespace deps | ✅ Fixed | `hardware/google/graphics/common` now the upstream main checkout that converted hwc3/libhwc2.1 to Soong |
| health AIDL V4 vs V5, `-pedantic`, fabricated kernel bp, zuma rename, system-partition props | ✅ Fixed | See CHANGELOG_port_20260902.md |
| `m nothing` success | ✅ Done | Panther and Cheetah green 2026-09-02 (`aosp_<device>-cp2a-eng`, Build Capture `completed_bootstrap`, 0 errors) |

### Secondary Task: Documentation
- [x] Update `README.md` with v0.4.0 enhancements
- [x] Create `CURRENT_STATE.md` (this document)
- [ ] Document known limitations and workarounds

---

## 🚧 Known Limitations & Future Work

### Current Limitations

| Limitation | Impact | Workaround |
|------------|--------|------------|
| `system_ext.img` contents not extracted | Vendor blobs inside system_ext missing | Manual mount/extract needed |
| Projects the target manifest ships must never be overwritten from AOSP 15 | Duplicate/undefined modules that surface far from the cause | Restore from the project's git HEAD; only copy directories absent from the target manifest |
| Kernel prebuilt dir is not in AOSP for cp2a | Board reads `RELEASE_KERNEL_<DEVICE>_DIR` | `create -stock -release cp2a` / `assemble-kernel` builds it from the factory image (pure Go, no root) |
| `simg2img` required | Vendor image extraction fails | `sudo apt-get install android-sdk-libsparse-utils` |
| Sudo required for mount | Only when `debugfs` is absent | Install e2fsprogs; `debugfs rdump` is tried first and needs no root |

### Future Enhancements

| Feature | Priority | Description |
|---------|----------|-------------|
| `system_ext.img` extraction | 🔴 High | Mount/extract system_ext.img contents |
| Full `m droid` validation | 🔴 High | Test physical device boot |
| `doctor` auto-apply | 🟡 Medium | Automated fix application from audit |
| Kernel prebuilt fetch | ✅ Done | `assemble-kernel` from the factory image (v0.4.0) |
| Android 16 support | 🟡 Medium | Test/update for AOSP 16 |
| Additional devices | 🟢 Low | Support for more Pixel devices |
| Docker/container support | 🟢 Low | Root-free path exists now (debugfs); containerizing is what remains |

---

## 📊 Progress Tracker

### Physical Device Revival Progress

```
Panther (Pixel 7) - Android 17 Port
├── [✅] Surgeon built and working
├── [✅] Factory image downloaded and verified
├── [✅] Outer ZIP extracted
├── [✅] Inner image ZIP extracted
├── [✅] Vendor.img contents extracted → vendor/google_devices/panther/proprietary/
├── [✅] Vendor_dlkm.img contents extracted → vendor/google_devices/panther/dlkm/
├── [✅] Device trees mirrored from embedded assets
├── [✅] Kernel prebuilt dir assembled from the factory image (6.1, build 26Q2-15260412)
├── [✅] Hardware subtrees mirrored
├── [✅] Lunch succeeds (aosp_panther-cp2a-eng)
├── [✅] Build reaches 2% completion
├── [✅] Undefined module dependencies fixed
├── [✅] m nothing succeeds (2026-09-02)
├── [⏳] m droid succeeds
└── [⏳] Physical device boot validation

Overall: 15/17 steps completed (88%)
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
#   panther 120519Z  cheetah 120945Z  lynx 121328Z  tangorpro 121701Z  felix 122559Z
#   raven 123025Z    bluejay 123425Z  tokay 123822Z  caiman 124121Z    oriole 124446Z
#   shiba 124842Z    husky 125137Z    akita 125430Z  komodo 125721Z    comet 130022Z  tegu 131105Z
# `m -j32 droid superimage` (full image): cheetah 152554Z, panther 153638Z, lynx 171442Z, tangorpro 223034Z
# boot: cheetah 2026-09-03 01:12Z — adb at 40 s, boot_completed at 70 s, Enforcing, encrypted, orange
```

---

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
4. [ ] Full builds: felix and oriole running (first foldable, first gs101) as the last data points; the other ten (bluejay, raven, shiba, husky, akita, tokay, caiman, komodo, comet, tegu) are **deferred by decision 2026-09-03 02:25Z** — no test device for those families, the `m nothing` gate already covers all sixteen, and the Pixel 7 family is proven end to end (four analysis gates, four full images, cheetah booted and toured). Build them later on demand: `./bin/aosp_build_capture -lane aosp17_<device> -jobs 16`, two at a time, then `assemble-super`.
5. [x] Cheetah camera — CLOSED as a fault of this unit, not the port (2026-09-03 02:18Z). Control test on stock (factory system, vendor and boot chain, matching firmware, captured with the same tool into `…/20260903T021809Z_boot_ok_stock-camera-black/`): 0 camera devices, 414 provider crashes, all 32 tombstones carry the identical abort `KRAKEN init error … LwisFence signal status: No such device or address`, rear and front cameras black. Our image's chain of evidence (kernel `lwis-sensor-kraken: I2C Write failed (-6)` → HAL abort loop → camera service add/remove → 0 devices → Camera2 app AIOOBE) is the same fault seen from an eng build. Boot images, module loading and bootconfig were each excluded on the way (factory boot chain over our super fails identically).

### Medium-term (This Month)
1. [ ] System_ext.img extraction (the 5–7 blobs per device the self-extractor lists inside system_ext.img — ShannonIms/Rcs, libmediaadaptor, UwbVendorService)
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
m nothing -j16
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
