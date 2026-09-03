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

# sovereign-lane-surgeon

**Seed a sovereign customization "lane" into an Android (AOSP) source tree — a parallel, first-class product variant that builds like stock, without overlays and without forking the whole tree.**

A single self-contained Go binary. Zero external dependencies. `go build` and run.

**v0.4.0** — stock-seeded lanes proven to a green full image. A whole-root fork of `frameworks/` +
`packages/` (178,264 files) now reaches `m droid` with zero errors in every phase. Getting there
surfaced sixteen distinct blocker classes; the automatable ones are applied by `create`, and all of
them are written up in [What a stock-seeded lane hits](#what-a-stock-seeded-lane-hits-and-why) so the
next lane — or the next reader — does not rediscover them one build at a time.

```
git clone … && cd sovereign-lane-surgeon
go build -o sovereign-lane-surgeon .
./sovereign-lane-surgeon create -name myui -devices lynx -out /path/to/aosp
```

---

## The problem

Customizing AOSP at the platform level has two established options, both painful:

1. **Runtime overlays (RRO)** — safe, but bounded. You can retheme resources; you can't replace `framework.jar`, reroute the build, or change behavior that isn't overlay-exposed.
2. **Fork the whole tree** — unbounded, but unmaintainable. You lose the ability to track upstream and drown in merge conflicts.

**Lane Sovereignty is a third path.** Your customization lives in *parallel* directories — `frameworks-<lane>/`, `packages-<lane>/` — and the build's file-finder does **per-file replacement**: for a `<lane>` build it keeps your lane's copy of each forked file and drops the stock parallel, so the build sees *"lane where you forked, stock everywhere else"* as one coherent tree. A lane is a real product variant (`aosp_<device>_<lane>`), not a hack layered on top of one.

This tool operationalizes the method: it wires the Soong routing, generates the device/emulator products, mirrors the subtrees you choose to fork, and rewrites the cross-references — so a working sovereign lane is one command instead of months of build-system archaeology.

---

## Two identity models

| | **keep-name** (default, Holo) | **app-naming / rename** (`-rename`, Model-A hybrid) |
|---|---|---|
| Identity apps | keep the stock name (`Settings`, `SystemUI`) | branded `<Prefix><Name>` (`NexusMSettings`) + `overrides:["<stock>"]` |
| Framework class | keep-name — the finder drops the stock parallel | **also keep-name** — finder-drop + suppressors (NOT stem/phony) |
| Shared infra / libs | keep-name | renamed `<Camel>-<lib>`, all dep-refs repointed in lockstep |
| Apex modules (`java_sdk_library`/`aidl_interface`) | stay shared-stock | stay shared-stock (Soong derives their names) |
| Collision avoidance | the finder drops the stock parallel per-file | branded identity names + finder drops the stock parallel |
| Best for | a faithful, whole-platform fork | a **branded** platform — named apps, keep-name plumbing |

Both build "like stock, unaware of the other lanes."

### Seeding a lane FROM another lane (`-from`)

```bash
./sovereign-lane-surgeon create -name holotest -from holo -out /path/to/aosp
```

`-from <lane>` seeds the new lane from an **existing lane** instead of stock — a staging lane for a
mature one, an experiment you can throw away, a variant that starts where a proven lane left off.
It defaults `-fork` to the source lane's roots, repoints its labels and paths onto the new lane,
**inherits its curated bp drop-list**, and **derives the emulator product from the source lane's own**
(app suite, soong namespaces, privapp mode) instead of leaving them as TODOs.

That last part is the load-bearing one. The finder computes a lane bp's stock parallel *from its
path*, which works right up until the source lane **moved** something:

| source lane has | stock parallel | computed | result without inheritance |
|---|---|---|---|
| `…/vts/kotlin/Android.bp` | `…/vts/java/Android.bp` | `…/vts/kotlin/` (absent) | stock stays loaded → *"module already defined"* |
| `packages-<src>/system/secretkeeper/…` | `system/secretkeeper/…` | `packages/system/…` (absent) | same |

Those drops cannot be derived from the new lane's tree — they exist only as the source lane's
accumulated curation, so a lane-sourced fork must inherit them.

**Blueprint files are not the only place the source lane's paths live.** A verbatim clone also
carries them in formats no `.bp` check can see, and those failures surface inside *generated* files:

| carrier | form | fails as |
|---|---|---|
| `.proto` | `import "frameworks-<src>/…/Resources.proto"` | a generated `.pb.h` whose `#include` names the source lane |
| `.cpp` / `.h` | `#include <frameworks-<src>/…/x.proto.h>` | missing header |
| `.mk` | copy-file paths | wrong file shipped |
| `.go` | lane soong logic setting `props.Visibility` to the source lane | grant to a lane this lunch never loads |

`create -from` rewrites these; `requalify -sources` does it standalone for an existing lane. Types
that are *not* build inputs (`.json` provenance records, `.html`, binaries) are reported, never
silently edited. The `soong_config` namespace is handled too — it is an identifier, not a path, so
no path rewrite reaches it, and a `select()` naming a namespace the new lunch never sets quietly
takes the stock branch of the lane's own lane-aware select.

### Lane names are path predicates

The finder matches lane content by **path-component suffix**, so `create` refuses a name that any
existing directory already ends with:

```
create: REFUSED — lane name "test" collides with 78 existing directories ending in "-test".
    frameworks/base/ravenwood/tests/minimum-test
    packages/modules/SdkExtensions/sdk-extensions-info-test
    ...
```

`-test` matches 107 stock directories, 15 with an `Android.bp` — including
`prebuilts/misc/common/androidx-test`. Every lane in the tree would stop seeing those directories, and the
failure surfaces somewhere else entirely (`external/robolectric` → *"depends on undefined module
androidx.test.core"*), naming neither the new lane nor the real cause. The check runs before any
file is written, because by the time a build disagrees the tree already holds a multi-GB clone and
patched shared soong sources.

The **app-naming / rename** model (`-rename`) renames only what should carry a brand and leaves the derived-name plumbing keep-name — the *Model-A hybrid*:

1. **Installables** — `android_app` → `<Prefix><Name>` with `overrides:["<stock>"]`.
2. **Libraries** — renames each lane library and **repoints every dependency reference in lockstep**, prop-family-aware (bare name in `static_libs`/`libs`/`defaults`/…, `:module` form in `srcs`/`tool_files`/…). Injects `owner:` on renamed `aidl_interface`.
3. **Framework class stays KEEP-NAME.** `framework-res`/`framework-minus-apex`/`services` are *not* renamed (no `stem`/`phony`): renaming them to `X-<lane>` + a `phony` bridge was proven to break the build — a phony `framework-minus-apex` does not implement `hiddenAPIModule` (so `platform-bootclasspath` fails) and a phony `services` is a non-java module (so `car-frameworks-service` fails). Instead the finder drops the stock parallel and the lane's keep-name module claims the canonical slot, exactly as in the keep-name model. The rename tier-3 machinery is retained (still unit-tested) but no longer wired into the pipeline.

The finder's app-naming routing does **per-file stock-parallel replacement with an identity de-prefix**: `packages-<lane>/apps/<Prefix>Foo/…` → drops `packages/apps/Foo/…`, so a branded app's stock parallel (and its keep-name test/internal twins that would otherwise collide in kati's flat namespace) drops in full. A *replaced* identity app drops its whole stock subtree (root + tests + no-lane-parallel subdirs); an *additive* shared-lib fork keeps its stock subtree; and an **apex-module subtree stays shared-stock entirely** (its api-snapshots are aggregated globally by `frameworks/base/api` and can't be prefixed).

### Optional: `-no-compose` (experimental)

`create -rename -no-compose` bypasses AndroidX/Jetpack Compose the way the Nexus-Modern lane does — an unconventional but powerful "View-in-Kotlin" premise. It leaves the Compose/AndroidX/Kotlin-vendored subtrees stock (computed from the real tree, not a stale list), auto-drops the lane bps' dangling Compose dep refs, and scopes SystemUI-class `srcs` to a re-authored `kotlin/**` tree (dropping `compose/**`/`src/**` + Compose `static_libs`). The build wiring is automated; the re-authored Kotlin itself is hand-written.

---

## Quickstart

```bash
# Seed a lane targeting one device, forking the framework class:
./sovereign-lane-surgeon create -name myui -devices cheetah \
    -fork frameworks/base,packages/apps/Settings -out /path/to/aosp

# Review the staged soong-patch diffs it prints, then commit them:
./sovereign-lane-surgeon apply -out /path/to/aosp

# Build (lunch is aosp_<device>_<lane>-<release>-eng):
m -j20 nothing        # graph-coherence gate
m -j20 droid          # full image

# Changed your mind? Reverse the whole seed, byte-for-byte:
./sovereign-lane-surgeon uninstall -name myui -out /path/to/aosp
```

- **`create`** resolves the device family + SoC from the tree (e.g. `cheetah` → family `pantah`, SoC `gs201`), generates the device/goldfish/cuttlefish products, mirrors + requalifies the forked subtrees, and *stages* the in-place Soong patches for review (preview-then-apply — it never edits your `build/soong` until you `apply`).
- Add **`-rename`** for the branded, side-by-side model (curated forks).
- **Up to 3 lanes at once:** `-name a,b,c` — each is registered so it's blind to the others.

---

## Architecture

```
create ─┬─ name guard      refuse a lane name any existing directory already ends with (the name
        │                   IS a finder path predicate) — checked before anything is written
        ├─ soong patches   finder.go (per-lane BP routing + exclude the lane from the stock-drop
        │                   guard), aar.go (isLaneLunch → framework suppressors), visibility.go
        │                   (lane↔stock pkg map), neverallow.go (mirror stock allowlist dirs onto
        │                   the lane)  [go/ast, AST-safe]
        ├─ device products  device/google/<family>-<lane>/aosp_<product>_<lane>.mk (+ SoC auto-fill)
        │                   goldfish + cuttlefish emulator products. BOARD subdirs are NOT copied:
        │                   kati globs '*/$(TARGET_DEVICE)/BoardConfig.mk' across all of device/,
        │                   and the lane keeps the stock PRODUCT_DEVICE, so a copied board dir
        │                   collides for every product of that device
        ├─ bp-mirror        clone forked subtrees (symlink-safe, .git-skipping)
        ├─ requalifier      //frameworks/base/…  →  //frameworks-<lane>/…   (fork-boundary aware,
        │                   AST-safe, form-preserving — vendored Blueprint parser). Covers labels
        │                   inside select() bodies, and with -paths also BARE root-relative paths
        │                   (include_dirs, aidl.include_dirs) + paths embedded in genrule cmd
        │                   strings. -paths is OFF by default because a SUBTREE fork rarely needs
        │                   it; a WHOLE-ROOT fork almost always does (584 refs across 137 bp on a
        │                   178k-file lane, vs 3 on the mature subtree-forked reference). It is
        │                   existence-guarded, so it is safe to run tree-wide
        ├─ lane-source      (-from only) repoint the clone off its source lane + inherit that
        │                   lane's curated bp drop-list (its directory relocations)
        ├─ stock-source     (no -from) relocate stock paths in NON-bp sources: .proto imports and
        │                   C/C++ #includes of GENERATED headers. Existence-guarded, and
        │                   PRODUCER-aware — a .pb.h/.proto.h never exists on disk, so the guard
        │                   tests the .proto that emits it (runRelocateStockSourcePaths)
        ├─ lane allowlists  DISCOVER shared-tree .go files carrying per-lane path lists (any Soong
        │                   plugin may keep one — external/icu does) and insert this lane beside the
        │                   existing entry. Discovery, not a fixed list: a .go holding a
        │                   "<root>-<existing-lane>/" literal IS an allowlist. AST-verified,
        │                   staged. Files the dedicated patchers own are excluded
        ├─ lane-tool fixes  repair build tools whose assumptions a HYPHENATED lane root violates —
        │                   streaming_proto's include guards, ProtoLogTool's hardcoded stock path.
        │                   Byte-exact match-or-skip; defects lane naming CREATES (laneToolFixes)
        ├─ rename pass      (-rename only) installables (+overrides) + libs+dep-repoint through the
        │                   Blueprint AST; framework-class stays KEEP-NAME (Model-A hybrid)
        ├─ no-compose       (-no-compose only) leave Compose/AndroidX subtrees stock, auto-drop dangling
        │                   Compose dep refs, scope SystemUI srcs to the re-authored kotlin/** tree
        ├─ stock patch      compatibility.mk license-metadata back-fill (lane-independent) so a
        │                   full-image / compat-suite build doesn't fail "<tool> has no license metadata"
        └─ route manifest   drops the known stock danglers → builds ZERO-FLAG (see below)

apply     ── commits the staged soong patches, snapshotting each file first
uninstall ── reverses a seed (byte-identical), scoped by -target:
               all     (default) dirs + shared products + AndroidProducts.mk + soong patches
               patches soong/make patches ONLY — the lane TREES stay on disk, so a lane can be
                       re-registered (renamed, re-seeded) without re-cloning multiple GB
               lanes   trees + products ONLY — the patches stay registered
             Inverse AST edits throughout; multi-lane-safe, leaves sibling lanes intact.
rename-module ─ AST-safe module-name rewrite across the lane tree: `name:` + every dep-ref (defaults/
             static_libs/libs/required/… + `:module` srcs) in one lockstep pass, form-preserving.
             PRIMARY USE — keep-name conformity DE-PREFIX: a lane that wrongly prefixed a
             framework-class / shared-infra module (a `java_defaults`, a shared lib) breaks the stock
             consumers that reference it by its BARE stock name once the lane loads keep-name-style;
             `-deprefix Nexusm -modules platform_service_defaults,…` de-prefixes the def + all lane
             consumers so those stock consumers resolve. `-map "old=new,…"` for arbitrary renames.
drop-dep   ─ AST-safe removal of named dep entries from every dep list + `:module` srcs ref across the
             lane tree. PRIMARY USE — the `-no-compose` premise: mirrored SystemUI bps carry stale refs
             to Compose/AndroidX libs the lane never builds; `-deps "//<lane>/…:X,bare-name,…"` drops them.
reexport   ─ THE RE-EXPORT IDIOM (app-naming). When the finder drops a full-REPLACEMENT identity app,
             the KEEP-NAME modules it EXPORTED (shared `java_defaults`, resource/plugin/statsd libs,
             `*-core`/`*-res`) vanish — breaking un-forked / cross-namespace consumers that still name
             them by keep-name (Launcher3, automotive, dev samples). This pass AST-detects those orphaned
             exports (matching the finder's drop decision exactly), keeps only the ones still referenced
             by the surviving graph, and emits minimal type-aware keep-name re-export stubs
             (`java_defaults`/`java_library`/`android_library`+manifest/`cc_library_shared`/`filegroup`)
             into `frameworks-<lane>/base/shared-app-defaults/Android.bp` (a location that collapses to
             the global namespace, so they resolve for both root-ns stock and lane consumers). Idempotent
             — regenerate whenever the fork set changes, instead of hand-authoring stubs one build-error
             at a time.
audit     ── classifies a build's failures by a blocker taxonomy → recipe per class
```

Every `.bp` rewrite goes through the real Blueprint AST (vendored `internal/blueprint/parser`), never a regex — so it can't corrupt module structure. The one makefile patch (`compatibility.mk`) is an exact byte-match-or-skip against the known stock block — never a fuzzy rewrite of brace-heavy make — and cleanly no-ops on an AOSP version whose block differs.

## Zero build flags

A complete sovereign lane builds with **no** `ALLOW_MISSING_DEPENDENCIES` and **no** `UNSAFE_DISABLE_APEX_ALLOWED_DEPS_CHECK`. Instead of blanket-absorbing every missing dependency (which masks *real* errors), the seeded route manifest **surgically drops the handful of non-lane stock modules that dangle in a typical checkout** — the deprecated OTA updater, cloud host-tool stubs, test infrastructure. Real lane dependency errors still surface; only the known cruft is dropped.

---

## Stock device revival (`create -stock`)

A lane forks a device that's still *present* in the target tree — `create -stock` instead **revives** a device family that isn't (dropped between AOSP releases, or never ported to a newer one). There's no stock parallel to drop here — the device simply doesn't exist in the target tree — so none of the lane machinery (finder routing, soong_config namespaces, `_<lane>` product suffixing) applies. Instead it mirrors the family **verbatim**, at its real stock path, no suffix:

```bash
./sovereign-lane-surgeon create -stock -devices lynx,tangorpro -release cp2a \
    -factory-images-root /path/to/factory-images -out /path/to/android-17.0.0_r1
```

- **No external AOSP tree required.** The binary embeds the device trees of every cp2a device family with an AOSP tree (~660MB, provenance per directory in `assets/aosp15_device.sources`) and the hardware HAL trees they reference; see "Every cp2a device with an AOSP tree is in the bundle" below.
- **`-source-root <tree>`** is optional, not required — point it at a live tree to override the embedded bundle (tried first, falling back to embedded for anything it doesn't have) for a fresher copy or a device the bundle doesn't cover.
- **`-kernel-version <ver>`** overrides `TARGET_LINUX_KERNEL_VERSION`; without it the version comes from the kernel inside the factory image (6.1 for the CP2A Pixel 7 family).
- **`-hw-subtrees <a,b,...>`** adds seeds to the reference closure; for the Pixel families nothing needs naming (see "Fully required is derived" below).
- **bp-parity check.** Every mirrored subtree is checked, file by file, against the reference it came from (embedded bundle or `-source-root`): any module the reference defines that an already-present target copy lacks is reported before you build. This catches the "plausible-looking stub" class that produces no error where it is introduced. **Limitation:** the reference is the AOSP 15 bundle, so the check cannot see AOSP 15 content that has overwritten a project the *target* release ships (see "Lessons from the android-17 revival" below).
- **`-factory-images-root <dir>`** wires real vendor blobs from an already-extracted factory image (`<dir>/<device>/...`) into `vendor/google_devices/<device>/`, following the exact `self-extractors_<device>/` (or `self-extractors/`) mechanism the stock tree ships — Google's own source for the "Vendor image, Binaries for AOSP" self-extracting installer (see `vendorblobs.go`). System-image-embedded blobs (anything living inside `system_ext.img` etc.) are reported, not silently skipped — they need separate filesystem-image extraction. `fetch-factory-image` (below) populates exactly the directory layout this flag expects.
- **`-release <rel>`** (e.g. `cp2a`) with `-factory-images-root` also **assembles the per-device kernel prebuilt directory** the target release names through `RELEASE_KERNEL_<DEVICE>_DIR` (cp2a → `device/google/pantah-kernels/6.1/26Q2-15260412`). That build is not published in AOSP (the public `pantah-kernels/6.1` project stops at android-15.0.0_r36), so the embedded bundle cannot carry it and the factory image is the only source: `boot.img` and `dtbo.img` verbatim, `Image.lz4` as boot.img's kernel payload, the first-stage modules and their load order out of `vendor_kernel_boot.img`'s LZ4-legacy cpio ramdisk, and the `vendor_dlkm` / `system_dlkm` module lists. Pure Go decoders (no `lz4` tool, no python module). It refuses to write a directory whose name (the flag) disagrees with the vendor modules' build id, keeps the *signed* copy when the same module ships in two partitions, and omits from `vendor_kernel_boot.modules.load` the modules the SoC board injects itself (`fips140.ko`), exactly as Google's shipped directory does. Standalone: `assemble-kernel -device <d> -release <rel> -out <root> -factory-images-root <dir>`.
- **The kernel a device really runs is read from the image, not assumed.** CP2A's Pixel 7 family runs GKI `6.1.157-android14-11 (ab14791245)` with vendor modules from Pixel build `ab15260412`; the `6.12` under `kernel/prebuilts` is the microdroid/cuttlefish GKI. `-kernel-version` should match the image (6.1).
- **Vendor extraction needs no root when `debugfs` (e2fsprogs) is installed**: every partition image is a raw ext filesystem after `simg2img`, and `debugfs -R "rdump / <dst>"` unpacks it unprivileged; the `sudo mount` path is only the fallback. Standalone: `extract-vendor -device <d> -out <root> -factory-images-root <dir>` (writes `proprietary/`, `dlkm/`, `system_dlkm/`).
- **"Fully required" is derived, not listed.** After the family directories land, every `device/google/<x>` and `hardware/google/<x>` subtree their makefiles and Blueprints reference is mirrored too, transitively (SoC dirs, sepolicy, gs-common, gchips/graphics/pixel), from whichever source has it. Subtrees that are git checkouts of the target tree are left to upstream (hardware/google/{interfaces,camera,av}); ones no source has are reported once (proprietary vendor trees). `-hw-subtrees` still adds seeds, but for the Pixel 7 family nothing needs to be named.
- **Target-release compatibility pass** (`targetcompat.go`, `overlays.go`) — the reason `create -stock` alone now reaches green on android-17. A device tree cut from one release carries idioms a newer one rejects; each operation answers one rejection observed on android-17.0.0_r1 and is **gated on a probe of the target tree**, never on a release number, so a tree that does not reject the idiom is left exactly as mirrored. All ten are idempotent and touch only the mirrored subtrees:
  1. *Illegal cflags* — the list is read out of the target's `build/soong/cc/config/global.go` (`IllegalFlags`, via go/ast) and dropped from every mirrored Blueprint (`-pedantic` in gs-common/gsa and pixel/recovery).
  2. *System-partition properties* — when the target's fsgen carries the artifact-path property check, `PRODUCT_SYSTEM_PROPERTIES` assignments in the mirrored product makefiles become `PRODUCT_PRODUCT_PROPERTIES` (the message's own suggestion; factory_*.mk excluded).
  3. *AIDL version conflicts* — a module that pins `X-V<n>-ndk` **and** links a sibling library from X's own Android.bp that pins `V<m>` (the translate library) is re-pinned to `V<m>` (AST rename, def + refs). Measured over every pin the bundle carries: exactly one module qualifies (libpixelhealth, V4→V5); a naive "re-pin to latest" would have touched 16 pins that build fine as they are.
  4. *Denylisted Android.mk* — android-17's `androidmk_denylist.go` blocks every makefile under device/google and hardware/google; r36's graphics/common still builds hwc3 and libhwc2.1 from three of them. Google's own Soong conversion (upstream 562ede8) travels with the binary as an overlay (`assets/overlays/MANIFEST`, five files, recorded with its commit) and replaces exactly those makefiles when — and only when — the target's denylist covers them. A blocked makefile that is only an include-wrapper with no makefile beneath it (Pixel 9a's r31 root `Android.mk`; the r36 families had already dropped theirs) is removed, since including it is a no-op. Any other blocked makefile with no overlay is reported as the blocker it is, never guessed at.
- **All 16 cp2a devices with an AOSP tree are `m nothing` green on android-17.0.0_r1 (2026-09-02)** from `create -stock` alone — panther, cheetah, lynx, tangorpro, felix (gs201); oriole, raven, bluejay (gs101); shiba, husky, akita (zuma); tokay, caiman, komodo, comet, tegu (zumapro). **Cheetah's full image (`m droid superimage`, super.img and all partitions) completed on 2026-09-02 (Build Capture run 152554Z)** after six more tree-probed operations and one bundle asset that the full build surfaced (operations 5 to 10 below and the kernel headers); the other fifteen full builds follow. **On 2026-09-03 cheetah booted that image** (lock screen in 70 s, adb, SELinux enforcing, encrypted /data) once the four flash lessons below were applied; the only defect found on the tour is a main-camera sensor that does not answer on I2C, a hardware-level fault outside the build.
- **Full images complete for every SoC generation cp2a ships (2026-09-03), one build at a time, all 9 `preflight` FLASHABLE:** cheetah, panther, lynx, tangorpro, felix (gs201) · oriole (gs101) · husky (zuma) · komodo and tegu (zumapro; tegu is the only device on the r31 tree). One device per family is built deliberately — the seven remaining siblings share those trees and their gates are green, and the host builds one image at a time.
- **Coverage status per device** lives in CURRENT_STATE.md ("cp2a device coverage"): which of the 16 devices are `m nothing` green, fully built, and flashed, with the Build Capture run ids as evidence.
- **Every cp2a device with an AOSP tree is in the bundle** (2026-09-02): device families raviole, bluejay, pantah, lynx, tangorpro, felix, shusky, akita, caimito, comet (android-15.0.0_r36) and tegu (Pixel 9a, whose tree exists only under android-15.0.0_r31) with their sepolicy and SoC dirs (gs101, gs201, zuma, zumapro), ~660MB; provenance per directory in `assets/aosp15_device.sources`. The factory-image manifest covers the same 16 devices. The Pixel 10 family (blazer, frankel, mustang, rango, stallion) has CP2A images but no AOSP tree in any tag and stays out of reach.
- **Lost transitive header exports** (`headerexports.go`, operation 5, from the first full build). `graphics/common/libion/ion.cpp` includes `<ion/ion.h>` and declares only liblog and libdmabufheap; in android-15 that worked because libdmabufheap linked libion statically and re-exported its headers, and android-17's libdmabufheap dropped libion while libion and its vendor-available `libion_headers` still exist. The op parses the target's provider Blueprint, and only when the export is really gone adds `header_libs: ["libion_headers"]` to the mirrored cc modules that depend on the provider and whose sources include such a header (the code calls no ION function, so headers are the whole gap).
- **Renamed proto options** (`protooptions.go`, operation 6, from the first full build). aprotoc rejected pixelstats' atoms 26 times with `Option "(android.os.statsd.module)" unknown`; android-17's `atom_field_options.proto` says why in its own text ("reserved 50004; module has been moved to module_name") and its atoms use `(module_name)`. The exact option token is renamed in the mirrored .proto files, only when the target's defining proto declares the new field and no longer declares the old one.
- **Platform-declared SELinux types** (`sepolicydecls.go`, operation 7, from the first full build). checkpolicy failed with `Duplicate declaration of type` for `vendor_chre_hal_prop`: android-15's gs-common declares it (via `vendor_internal_prop`) and android-17's `system/sepolicy/vendor/property.te` took it over. A type may be declared once and the platform's declaration is the one the rest of the tree is written against, so the mirrored declaration line is dropped whenever the target's live `system/sepolicy` declares the same name (its `private/file.te` also took `per_boot_file` from the gs101/gs201 policies); rules that use the type stay — unless the platform declares it in `private/`, where vendor policy cannot see it: then the vendor rules on it and the file_contexts lines the platform already carries go too (`per_boot_file`: android-17's private kernel.te, toolbox.te and file_contexts hold the very rules gs101/gs201 shipped; leaving them gives `unknown type`). Candidates come from the pristine bundle, not the target's copy, so a re-run over a treated tree still finds them. Scoped to types the pass itself un-declared, never a sweep over private types. Measured on android-17: two types, 18 lines.
- **Neverallowed vendor statements** (`sepolicyneverallow.go`, operation 8, from the first full build). secilc's final check named two android-15 vendor statements android-17's platform forbids: gs-common's `allow dumpstate vold:binder { call };` (17 adds `neverallow dumpstate { vold }:binder call`) and `binder_call(vendor_pcs_app, hal_pixel_remote_camera_service);` (a binder call to a service *name*, rejected by `neverallow * { -domain }:binder *`). A neverallow cannot be computed without the policy compiler, so the evidence is data: `assets/sepolicy_neverallow/MANIFEST` records each statement with the platform rule that forbids it, and the statement is dropped only while the target carries that rule. `audit` classifies a fresh `neverallow check failed` as sepolicy-neverallow-violation with the same recipe.
- **`-Werror` under a newer toolchain** (`werror.go`, operation 9, from the first full build). pixelstats failed on two `unused variable` diagnostics promoted to errors, in a file byte-identical to Google's current upstream tree: the code is right for its tag, android-17's clang (r584948 versus r36's r536225) simply diagnoses more. Dropping the module's own `-Werror` is not enough — `build/soong/cc/compiler.go` injects `-Werror` into every compiling module outside `WarningAllowedProjects` (`device/`, `vendor/`) that declares neither `-Werror` nor `-Wno-error`, and records `-Wno-error` as the sanctioned opt-out. So for mirrored compiling cc modules under hardware/google the exact `-Werror` is removed and `-Wno-error` added (66 Blueprints), only when the target's `ClangDefaultVersion` (go/ast) differs from the bundle tag's (`assets/aosp15_device.toolchain`); modules under `device/` are left alone since Soong already allows their warnings. Warnings still print; sources stay Google's byte for byte.
- **HALs floating on a newer AIDL version than they declare** (`aidlfloat.go`, operation 10, from the first full build). power-libperfmgr builds with `defaults: ["android.hardware.power-ndk_shared"]`, a Soong defaults module that always names the interface's newest version (V6 at r36, V7 at 17); the HAL's support tables know V6's four SessionModes and its own `static_assert` refuses V7's five, while its vintf fragment states `<version>6</version>` and Google's public tree has not moved it. So a module whose fragment declares interface X at V<N> and floats on `X-ndk_shared`/`_static` is pinned to `X-V<N>-ndk` (shared_libs/static_libs, file-wide for consistency) when the target's defaults, evaluated from their Blueprint variables, resolve above N and the target still ships V<N>. Interfaces the fragment does not declare keep floating: no evidence.
- **Cross-tag reconciliations** (`reconcile.go`, `assets/reconcile/MANIFEST`). The bundle is one tree from two tags, and a family from the other tag can include a makefile the shared tag removed: tegu's device makefile includes `hardware/google/pixel/vibrator/cs40l26/device.mk`, a directory r36 dropped entirely (it moved the flag modules to gs-common under the same names and every r36 device gets the HAL as the vendor blob `android.hardware.vibrator-service.cs40l26`, which tegu's factory image ships too). Supplying r31's directory instead was tried and duplicates the aconfig package (Soong panics), so the faithful move is the one r36 made for every family: the listed include line is dropped, only when its target is really absent, with the evidence recorded in the manifest.
- **The assembler follows each SoC's kernel layout.** gs101 (Pixel 6/6 Pro/6a) has no `vendor_kernel_boot.img`: its first-stage modules and its device tree live in `vendor_boot.img`'s `dlkm` ramdisk fragment and the board reads `modules.load`; gs201/zuma/zumapro read `vendor_kernel_boot.modules.load` (AOSP 15 shipped both names with identical content, and so does the assembler). The dtb section of the vendor boot image is written as `<image>.dtb` so the build's `dtb.img` rule (`BOARD_PREBUILT_DTBIMAGE_DIR/*.dtb`) has its input. Kernel headers are NOT optional for a full build: `TARGET_BOARD_KERNEL_HEADERS` points at `<kernel dir>/kernel-headers`, config.mk's `$(wildcard)` silently empties it when absent (so `m nothing` passes), and the graphics HAL then fails on `<drm/samsung_drm.h>`. No factory image carries headers, so the bundle holds each family's set from the last AOSP tag that shipped its kernel dir (`assets/kernel_headers/MANIFEST`: r36's 25Q1 build; tegu's from r31's 25D4) — 9 to 10 files, byte-identical across the families of one SoC — and `assemble-kernel` writes them into the assembled dir with a provenance note.
- **Flashing an android-17 image of a prebuilt-vendor device** (`assemble-super`, `flash_<device>.sh`, `preflight`; learned on cheetah, 2026-09-03). Four facts, each from a failed boot: (1) the built super.img held no vendor/vendor_dlkm (lpdump showed no vendor_a). The first reading blamed android-17's Soong super; the ninja graph says super.img is Make's, and Make's `filter-out-missing-vendor` drops the prebuilt partitions whenever `INSTALLED_VENDORIMAGE_TARGET` is unset — identical in 15 and 17. It was unset because the vendor glue had been written flat while `device/google/<family>/<device>/BoardConfig.mk` includes `vendor/google_devices/<device>/proprietary/BoardConfigVendor.mk` (and device-<device>.mk inherits `proprietary/device-vendor.mk`; all 16 cp2a devices, checked): `BOARD_PREBUILT_VENDORIMAGE` never reached the build, nor did the vendor vbmeta chain, the firmware requirements, or device-partial.mk's packages. The placement is now read from the including tree (`glueDestination`), a flat copy is moved, `preflight` reports a flat copy as FAIL, and `assemble-super` packs the prebuilt partitions only for a tree that predates the fix; (2) vbmeta must be flashed exactly as signed — `fastboot --disable-verification` rewrites the flags in flight, breaks the signature, and this bootloader then hands init no `androidboot.vbmeta.*`, so first-stage init dies and the phone shows "Preparing For Ramdump"; (3) in bootloader mode `fastboot -w` only erases userdata and metadata (reported as raw), so they are formatted as f2fs explicitly; (4) the bootloader and baseband must be the ones the vendor blobs require — the vendor wiring takes android-info.txt from the factory image (the self-extractor's copy names a 2022 bootloader) and enables it, so the build's android-info.txt carries the real requirements and fastboot refuses a mismatch; with older firmware userspace dies at ~65 s regardless of what is in super. The generated script encodes all four; the firmware flash itself is left to the operator.
- **Bundle fidelity is proven, not assumed (`bundle audit -source <aosp tree>`).** `go:embed` is a lossy container: it carries regular-file content and nothing else. Two losses were found the expensive way, each patched with its own manifest and its own generator, which closes an instance and not the class — nothing said what *else* was missing. `bundle audit` walks a real source tree beside the bundle and reports every divergence by class: missing or extra files, content, executable bits, symlinks and their targets, empty directories, and any entry the bundle cannot represent at all. Exclusions are declared in code, never silent (`.git` gitfiles; `<family>-kernels/`, excluded on purpose). Measured 2026-09-03 against `android-15.0.0_r36`: **9671 entries, 32 directories, zero divergence**; tegu's two directories report as unaudited because their r31 source is not on this machine, which is the honest answer rather than a pass. One generator (`cmd/bundlemanifest`) now emits all four manifests from one walk under one `go generate`, so a new kind of loss cannot be left without something regenerating it.
- **`go:embed` drops symlinks, and AOSP uses them as headers** (`assets/aosp15_device.symlinks`, `cmd/symlinkmanifest`, from the first zuma build). embed.FS carries regular-file content only: it loses the executable bit (solved earlier with `assets/aosp15_device.exec`) and it loses symlinks *entirely* — the walk never yields them. The bundle has five, four of which matter: `hardware/google/graphics/{zuma,zumapro}/include/displaycolor/displaycolor_gs101.h -> ../gs101/displaycolor/displaycolor_gs101.h` and two gchips gralloc headers. Nothing notices until a build compiles libacryl, whose `include_dirs` make zuma's own `displaycolor_zuma.h` resolve `#include <displaycolor/displaycolor_gs101.h>` through that link — husky then died with "file not found" 46 minutes in (run 083328Z). The links are now a generated manifest, recreated after the files they point at (`materializeSymlink`, copying the target's bytes if a filesystem refuses a link), carried as real symlink entries in `bundle export`, and pinned by a test that reads the checked-out assets tree. **This is why one build per SoC family is worth the machine time:** the gate is blind to it, and so are five gs101/gs201 full builds.
- **`compat-propose` earned itself on the first try** (zuma, 2026-09-03). Husky's rebuilt image failed secilc's neverallow check: `binder_call(hal_radioext_default, gril_antenna_tuning_service)` in `device/google/zuma-sepolicy`, forbidden by 17's `neverallow * { -domain }:binder *` — the same class as cheetah's camera case, because `gril_antenna_tuning_service` is a `service_manager_type`, a service NAME, not a domain. secilc reports it in CIL form and names *both* source files and lines, so the detector reads the statements straight from them rather than searching the tree; `compat-propose -report … -out … -write-to .` wrote the manifest row with no hand-reading. Scanning every `binder_call` target declared `service_manager_type` across the mirrored policy then found the same statement in caimito's three `grilservice_app.te` files, so those rows went in before komodo could fail on them. The legitimate `:service_manager find`/`add` rules on that service are untouched.
- **The seed checks itself (`verify-seed`, run automatically by `create -stock`).** Every defect this port hit was a seed that *looked* complete: the vendor glue one directory from where the device tree includes it, a blob flat where a Blueprint named it under `lib64/`, a symlink that never arrived. None is visible in the seed log, `m nothing` is green through all of it, and the bill arrives 25 to 46 minutes into a full build or at a black screen. So the seed now verifies its own output in about a second, and every check is *derived from the tree* rather than a per-device list, so it keeps working for a device, family or release this code has never seen:
  - every vendor makefile a bare `include`/`inherit-product` names exists (an `-include` / `inherit-product-if-exists` path may legitimately be absent — tegu names three that only exist in an internal build, and Make distinguishes the two);
  - **no vendor makefile exists that nothing includes.** This is the one that catches a misplacement: the file was *there*, and every include of it was the optional form, so only reachability tells the truth. `Android.mk`, `AndroidProducts.mk` and `CleanSpec.mk` are exempt, being found by the build system rather than included;
  - every blob the vendor makefiles' `PRODUCT_COPY_FILES` and the vendor Blueprints' `srcs` name is where they name it;
  - every executable bit and symlink the bundle records for a mirrored subtree survived, and each link whose target is inside the bundle resolves (a link pointing out into the wider tree, like pixel's `.clang-format`, is not held to that);
  - the kernel prebuilt directory the release names is populated, and every Blueprint under the device and vendor trees still parses.

  Each of the three real defects was replayed against it: all three are caught, by name. `-no-verify` opts out.
- **`preflight`: what the images can prove without a phone.** The boot proof exists for one SoC generation (cheetah, gs201). For a family with no test device the next best evidence is the built images measured against the factory image they came from, made mechanical: kernel release read from both boot images; the super's partition set ⊇ the factory `super_empty.img` layout and each group within its maximum (liblp metadata parsed in Go after a sparse-aware prefix read — the build's own lpdump refuses the sparse super); every partition the factory's vbmeta verifies and the build produces is verified by the build's vbmeta (avbtool info_image, both sides); the build's android-info.txt carries the factory's `require version-*` lines; the vendor glue in place. PASS/FAIL/SKIP per check with the measured values, exit 1 on any FAIL. Run on cheetah's pre-fix images it reported exactly the two facts that were wrong (android-info, glue) and nothing else.
- **System_ext blobs are wired.** The extract list names 5–7 files per device inside `system_ext.img` (ShannonIms/Rcs, UwbVendorService, their permission xmls, libmediaadaptor); `create -stock` now unpacks system_ext.img like the other partition images (debugfs, no root) and copies them where their consumers name them (`blobDestination`: device-partial.mk's PRODUCT_COPY_FILES, Android.bp.txt's srcs — libmediaadaptor under `lib64/`), and installs the staging `Android.mk.template` / `Android.bp.txt` as `proprietary/Android.{mk,bp}`. Images are unpacked before the blobs are wired.
- **Kernel version from the image.** With `-release`, `TARGET_LINUX_KERNEL_VERSION` is set from the kernel the factory image actually carries (6.1 for CP2A's Pixel 7 family) unless `-kernel-version` overrides it; sibling products nobody named are untouched.
- **Replayed end to end (2026-09-02).** Every surgeon-owned directory was deleted from the android-17.0.0_r1 tree and `create -stock -devices panther,cheetah,lynx,tangorpro -release cp2a -factory-images-root …` recreated them in 2.5 s with no hand edit: 11 subtrees by reference closure, the four compat operations on exactly the seven files that had been fixed by hand that morning, plus the graphics/common overlay, kernel dirs and 6.1 lines from the images. Verdict: `m nothing` green on that tree for all four — panther 20260902T120519Z, cheetah 120945Z, lynx 121328Z, tangorpro 121701Z (completed_bootstrap) (the first attempt, 120133Z, failed on the denylisted graphics/common Android.mk — that failure is what produced operation 4).
- **Executable bits survive the bundle.** Go's `embed.FS` keeps contents only (every embedded file stats 0444), so `create -stock` used to materialize AOSP's scripts as 0644 and tangorpro's `m nothing` died at kati: `device/google/tangorpro/uwb/country_conf_gen.sh: Permission denied` (its `uwb_calibration_country.mk` re-runs the generator via `$(shell)` to prove itself current). `assets/aosp15_device.exec` (regenerated by `go generate ./...`, i.e. `cmd/execmanifest`) lists the 26 executables and `materializeEmbedded` writes them 0755; `copyFile` (the `-source-root` mirror and soong staging) now preserves the source's permission bits too. A test fails if the manifest and the assets tree disagree.
- **A mirror never writes into a git checkout.** If a target subtree under `-out` carries a `.git`, it belongs to upstream and the bundle would only re-add files upstream deleted (AOSP 15's `Android.mk` files into a `graphics/common` replaced by upstream main); the mirror refuses and says so.
- **Lane creation benefits too**: `create` (no `-stock`) now seeds `device/google/<family>` from the same embedded bundle first if `-out` has no such family at all yet, before lane-ifying it — a lane for lynx/tangorpro/pantah needs no external tree either.

---

## Fetching factory images (`fetch-factory-image`)

The partition images `-factory-images-root` needs (`vendor.img`, `boot.img`, `vendor_dlkm.img`, ...) come from Google's official Pixel factory images — large (~2-4GB), individually licensed, per-build binary downloads, not something this project embeds or redistributes (Google's terms explicitly prohibit that). `fetch-factory-image` automates getting them anyway, without either redistributing Google's binaries or scraping their site:

```bash
./sovereign-lane-surgeon fetch-factory-image -device panther -out /path/to/factory-images
# prints Google's real terms, requires typing "I agree", then downloads + verifies + extracts
./sovereign-lane-surgeon create -stock -devices panther -out /path/to/aosp-17 \
    -factory-images-root /path/to/factory-images   # the two compose directly
```

- **Not a live scrape.** developers.google.com/android/images' download table is populated by client-side JS after an explicit "Acknowledge terms" click and simply isn't present in the page's raw HTML (confirmed: a plain HTTP GET returns a ~75KB shell with zero `dl.google.com` references) — rendering it would need a headless browser, a dependency this self-contained tool deliberately doesn't take on. Instead, `fetch-factory-image` ships a small hand-verified manifest (`factoryImageManifest` in `fetchfactoryimage.go`) of the exact `dl.google.com` URL + published SHA-256 for each of the 4 devices this project targets, captured by actually opening the page and reading the table. The URLs themselves are plain, unauthenticated static files once known (confirmed: HTTP 200, `Accept-Ranges`, no cookie/session) — so no scraping is needed to use them, only to *discover new ones* (Google publishes new builds — typically monthly — which means refreshing the manifest by hand; see the comment above it for the exact steps).
- **Consent is reproduced, not bypassed.** The command embeds Google's real terms text verbatim and won't download anything until the user types "I agree" (or passes `-i-accept-google-terms` for scripted use, printing the terms regardless so they're on record) — the same gate Google's page enforces, faithfully mirrored rather than routed around.
- **Downloads FROM Google TO the user's own machine only** — nothing is cached or redistributed by this project; each run fetches directly from `dl.google.com`.
- Resumable (`Accept-Ranges`-aware), SHA-256-verified against the manifest, then extracts the nested outer-zip → `image-*.zip` → partition-images structure automatically (zip-slip-safe extraction). Writes `<out>/<device>/`, matching a manually-extracted factory image's layout exactly — drop straight into `-factory-images-root`.
- **v0.4.0: vendor image content extraction.** After extracting partition images, `fetch-factory-image` (and `create -stock -factory-images-root`) also mounts `vendor.img` and `vendor_dlkm.img` and copies their contents out: sparse detection via `file`, `simg2img` to raw, `sudo mount -o loop`, `rsync`/`cp`, unmount, `sudo chown`. Contents land in `vendor/google_devices/<device>/proprietary/` and `.../dlkm/`.
  - **Where they land depends on the command.** `fetch-factory-image` writes that `vendor/` tree under its own `-out` directory; `create -stock -factory-images-root` writes it under the AOSP root. Only the latter is what the build sees.
  - **What the build actually consumes** is the self-extractor mechanism: `BoardConfigPartial.mk` points `BOARD_PREBUILT_VENDORIMAGE` / `BOARD_PREBUILT_VENDOR_DLKMIMAGE` at the *partition images* in `proprietary/`. The loose extracted files sit beside them for inspection and for the system_ext-category entries that still need hand placement; nothing in the generated makefiles references them yet.
  - Requires `simg2img` (`android-sdk-libsparse-utils`) and `sudo`.

## Complete Device Revival Workflow (v0.4.0)

From a factory image to a booting phone, as proven on cheetah (Pixel 7 Pro) on android-17.0.0_r1, 2026-09-03:

```bash
# 1. Fetch the CP2A factory image (16 devices in the manifest; resumable, SHA-256 verified)
./sovereign-lane-surgeon fetch-factory-image -device cheetah -out /path/to/factory-images -i-accept-google-terms

# 2. Revive the device: mirrors the family and everything it references, assembles the kernel dir from the
#    image, reads the kernel version from it, wires the vendor blobs with their firmware requirements,
#    and runs the ten target-compat operations the target tree is probed to need. ~3 s. No hand edit.
./sovereign-lane-surgeon create -stock -devices cheetah -release cp2a \
    -factory-images-root /path/to/factory-images -out /path/to/android-17.0.0_r1

# 3. Build through AOSP Build Capture (run-dir name is the verdict). ONE build at a time: two -j16 builds
#    plus a gate exhausted a 62GB laptop and crashed the editor (2026-09-03); run lanes sequentially, and
#    from a detached runner (setsid nohup) so no session timeout kills a build mid-way. With the host to
#    itself the full lanes default to -j20 (32 cores; -j16 measured 14GB still available).
cd /path/to/AOSP_Build_Capture && ./bin/aosp_build_capture -lane aosp17_cheetah_nothing   # analysis gate, ~4 min
./bin/aosp_build_capture -lane aosp17_cheetah                                              # m -j20 droid superimage, ~1.5 h

# 4. Measure the images against the factory image before touching the phone (exit 1 on any FAIL)
./sovereign-lane-surgeon preflight -device cheetah -out /path/to/android-17.0.0_r1 -factory-images-root /path/to/factory-images
#    kernel release = factory's; super holds every partition of the factory layout and fits its group;
#    vbmeta verifies every factory-verified partition the build produces; android-info.txt names the
#    blobs' firmware; the vendor board glue is where the device tree includes it from.

# 5. The flash script. The build's own super.img is complete once the vendor board glue reaches the
#    build (see "Flashing" below); assemble-super then only writes the script, and packs the prebuilt
#    partitions into super_full.img only for a tree revived before that fix.
./sovereign-lane-surgeon assemble-super -device cheetah -out /path/to/android-17.0.0_r1
#    → flash_cheetah.sh (firmware requirements, boot chain, vbmeta as signed, super, explicit f2fs
#      format of userdata/metadata, reboot)

# 6. Flash (phone unlocked, in fastboot). The bootloader/radio the blobs require are flashed by you first
#    if the phone's are older — the script prints the versions and the two commands.
bash /path/to/android-17.0.0_r1/out-aosp17/cheetah/eng/target/product/cheetah/flash_cheetah.sh

# 7. Tour / capture (Build Capture's runtime sibling; adb is the build's own host tool)
PATH=/path/to/android-17.0.0_r1/out-aosp17/cheetah/eng/host/linux-x86/bin:$PATH \
  ./bin/aosp_runtime_log_capture -device <serial> -lane aosp17_cheetah -label first-boot -pull-tombstones
```

Measured on cheetah (2026-09-03, second proof, on the build's own complete super): adb at 21 s, `sys.boot_completed`
at 48 s, SELinux enforcing, /data encrypted, orange verified-boot state, vendor and vendor_dlkm mounted through
dm-verity under the build's vbmeta_vendor chain, baseband reported, 75 hardware services registered, and the
IMS/RCS/UWB vendor apps installed on system_ext from the wired blobs (first proof, 2026-09-03 01:12 UTC: adb 40 s,
boot 70 s, on `assemble-super`'s packed super). Steps 1–5 and 7 are the toolkit's;
step 6 is deliberate operator work (firmware and a wipe).

## Distribution, growth and drift (2026-09-03)

Five weaknesses named after the cheetah boot, each answered in code:

- **The binary carried the bundle (510MB).** The bundle is now content-addressed data (`bundle.go`): the binary always embeds the *manifest* (`assets/aosp15_device.sha256`, sha256+size of all 7130 files, `go generate`) and `bundle id` is its hash. The content is embedded by default, or left out with `go build -tags nobundle` (15MB) and supplied at run time — `create -bundle-dir`, `$SLS_BUNDLE_DIR`, the cache (`~/.cache/sovereign-lane-surgeon/bundle/<id>/`), or `create -bundle-url` / `$SLS_BUNDLE_URL` fetching the archive `bundle export` wrote. Every non-builtin source is verified file by file against the manifest before use (a stamp records a verified directory); an archive that does not match the manifest this binary was built with is refused and removed, whatever its name.
- **Operations 5, 6 and 8 were tables in Go.** They are manifests now (`assets/compat/header_exports.MANIFEST`, `assets/compat/proto_options.MANIFEST`, `assets/sepolicy_neverallow/MANIFEST`), and `compat-propose -report <failed run> -out <root>` writes the rows from a failure: for `'x/y.h' file not found` it finds the `cc_library_headers` in the target whose `export_include_dirs` hold the header and proposes one row per candidate provider among the failing module's deps (a row whose provider still exports is a no-op by the operation's own probe); for `Option "(pkg.name)" unknown` it finds the proto with that package extending the option types and the `name_*` field that replaced it; for a secilc `neverallow … violated by allow …` it finds the mirrored `.te` line carrying the statement (or the macro naming both parties) and the platform line. `-write-to <surgeon src>` appends them; the binary is rebuilt after.
- **Two truths about super.** Resolved at the root (the glue placement above): the build's super.img is complete, `assemble-super` is the fallback.
- **Docs drifted.** `docs_test.go` fails the suite when README/CURRENT_STATE disagree with the code: the version, every subcommand in main's dispatch (README and `usage()`), the number of compat operations (`targetCompatOperations`), every device in the factory manifest, and a provenance tag for every bundle directory. It found `reexport` missing from `usage()` on its first run.
- **One SoC generation proven.** Closed as far as images can prove it: gs201, gs101, zuma and zumapro all built full images from the same bundle and the same code path, each passing every `preflight` check — including gs101's different layout (no init_boot, no vendor_kernel_boot, no system_dlkm), which preflight derives from that device's own factory image rather than assuming cheetah's. Husky is the evidence the exercise was worth it: three attempts, and both failures were toolkit bugs the analysis gate cannot see. A **boot** proof still exists only for gs201, and needs a phone.

## Status

- ✅ **Keep-name sovereignty (zero-flag)** — proven end-to-end on a real tree at two scales: a subtree
  fork of `frameworks/base` (~46k files) and a **whole-root fork of `frameworks/` + `packages/`
  (178k files)**, both reaching zero-flag, zero-error Soong analysis + kati; the latter also reaching
  a full green `m droid`. Every generated artifact is verified against the hand-built reference lane
  it mirrors — **and that reference lane is the single most useful debugging instrument this tool
  has.** When a stock-seeded lane misbehaves, diff it against a mature lane before theorising: the
  mature lane is a working instance of the thing you are building.
- ✅ **App-naming / rename (Model-A hybrid)** — installables + libs+dep-repoint AST-validated (round-trip clean on real SystemUI / `frameworks/base` / `services` blueprints); framework-class is keep-name (the stem/phony tier retired after it was proven to break `platform-bootclasspath`/`services`). Drove a real branded lane (Nexus-Modern, `frameworks-nexusm`/`packages-nexusm`, ~40 forked apps) to **zero-error `m nothing` Soong analysis**.
- ✅ **`reexport` idiom** — one pass over the Nexus-Modern lane detected 41 replaced-app stock subtrees and auto-generated 35 correct keep-name re-export stubs, replacing hand-authored graph-coherence stubs.
- ✅ **`-no-compose`** — AndroidX/Compose-bypass wiring (exclude-gen + auto drop-dep + SystemUI srcs-scope), unit-tested.
- ✅ **`uninstall`** — byte-identical seed reversal, proven end-to-end; multi-lane-safe. ⚠️ It removes the lane **trees** as well as the patches — a full reversal, not a patch-only undo.
- ✅ **Lane-sourced fork (`-from`)** — proven end-to-end: a 157,119-file lane cloned from a mature one reaches a zero-error `m nothing`, **with the source lane still green on a re-verify** (mutual isolation, both directions, measured — not assumed).
- ✅ **Full `m droid` — PROVEN (2026-08-20).** A **stock-seeded** lane (`holo2test`: whole-root fork of
  `frameworks/` + `packages/`, **178,264 files**) reached a **green `m droid` emulator image** —
  `[100% 35283/35283]`, zero soong/kati/javac/ninja errors, `system.img` 891M + `super.img` 1.8G.
  This validates forked **content and toolchain**, not just graph coherence. Sixteen distinct blocker
  classes were found and closed getting there; every one is documented in "What a stock-seeded lane
  hits" below, and the automatable ones are now applied by `create`.
  ⚠️ `dir_status` may report `completed_noop` on an INCREMENTAL run of a full build — the classifier
  keys on ninja step count and under-reports when most steps are cached. Trust `[100% N/N]`, zero
  failures, and the presence of the images.
- ✅ **Namespace collapse in the keep-name apply** — a lane subtree declaring its own `soong_namespace` holds its modules out of the global namespace, so stock consumers naming them bare stop resolving. The keep-name route function now drops those declarations (keeping only genuine pods), matching the rule the mature lane arrived at — verified against that lane's real `Android.bp.list`, 11/11.
- ✅ **The fork-everything flat-namespace tail — CLOSED for the stock-seeded case (2026-08-20).** The
  proto and FQ-label refs this line used to flag are now handled: `.proto` imports and generated-header
  `#include`s by `runRelocateStockSourcePaths`, qualified labels by the namespace-aware `requalifyPath`
  (with the `/pods/` carve-out), bare + embedded paths by `requalify -paths`. Verified by a green
  `m droid` on a 178k-file whole-root fork.
- 🚧 **`reexport` on a whole-root fork** — `reexport` is proven for the app-naming model (41 replaced
  subtrees → 35 stubs on Nexus-Modern). It was never exercised by the stock-seeded keep-name bring-up,
  so its behaviour there is untested rather than known-good.
- 🚧 **Folding the live finder's app-naming routing** (root-disposition / full-subtree-drop / apex-keep)
  back into the generation template.
- 🗺️ **Roadmap** — a `doctor` that auto-detects danglers from build evidence, richer device-fork auto-fill.
- ✅ **Stock device revival (`create -stock`)** — proven end-to-end reviving Panther/Cheetah (shared `pantah` family) and Lynx from a from-scratch `-out`, zero external AOSP tree: family + `-sepolicy` mirror, multi-product family de-dup, kernel-version reconciliation, and hw-subtree mirroring all verified idempotent (a re-run no-clobbers cleanly). Vendor-blob wiring (`-factory-images-root`) implemented against the real `self-extractors_*` mechanism; the system_ext.img-embedded blobs still need separate filesystem-image extraction (reported, not silently dropped).
- ✅ **`fetch-factory-image`** — hand-verified manifest (4 devices, real dl.google.com URLs + SHA-256) with resumable download, checksum verification, zip-slip-safe nested extraction, and **vendor image content extraction** (v0.4.0). Unit-tested (extraction, inner-zip discovery, zip-slip rejection, checksum mismatch). End-to-end run on real Panther factory images (2026-09-01).
- ✅ **android-17 device revival at scale** — all 16 cp2a devices with an AOSP tree are `m nothing` green on android-17.0.0_r1 from `create -stock` alone, and full images (`m droid superimage`) have completed for cheetah, panther and lynx; the per-device evidence lives in CURRENT_STATE.md and the operations that made it possible are described above.

Self-contained, **112 tests, zero external dependencies** (`go test ./...`).

`verify`/`audit` treat every Build Capture success status as green, including `completed_bootstrap`, the `m nothing` graph-coherence gate this loop runs on. `cmd/bpdropcflag` removes an exact cflag from every `cflags`/`cppflags` list in a Blueprint through the AST (android-17 rejects `-pedantic`, which AOSP 15 device trees still carry).

## What a stock-seeded lane hits (and why)

**Read this before forking whole roots.** `create -from <lane>` inherits a mature lane's accumulated
fixes. `create -fork frameworks,packages` — seeded from **stock** — inherits none of them, and that is
usually the *point* (a clean control for measuring a transformation). The price is re-encountering
every defect the mature lane already solved, plus a class of defect the sovereignty model itself
creates. All sixteen below were found bringing `holo2test` to a green `m droid`.

### The organising insight: a forked tree carries its origin's identity where no `.bp` check can see

Requalifying blueprints is the easy half. A verbatim clone also carries stock paths in `.proto`
imports, C/C++ `#include`s of *generated* headers, `aidl.include_dirs`, hardcoded strings inside
build tools, and file **modes**. Each fails *remote from its cause*, and — the part that costs
cycles — **each needs a DIFFERENT existence predicate**:

| reference form | what proves the lane owns the target | resolved by |
|---|---|---|
| `//path:mod` label | the lane bp is **in the routing receipt** (`.module_paths/Android.bp.list`) | Soong, against the loaded graph |
| bare / embedded path (`include_dirs`, genrule `cmd`) | the lane **directory** exists | the compiler, against files on disk |
| `.proto` import | the lane **`.proto`** exists | protoc |
| `#include "*.pb.h"` / `"*.proto.h"` | the lane **`.proto`** exists — *never* the header | the compiler |
| executable script | the source **mode** bit | the OS |

Two traps live in that table. A `//` label and a bare path *look identical* but are resolved by
different things — a label into a namespace-declaring dir must stay **stock** (the finder keeps the
stock parallel), while a bare path into the same dir must be **relocated** (files are on disk
regardless of routing). And a generated header **never exists in the source tree**, so an on-disk
test against it silently declines every rewrite it should have made.

### The sixteen classes

**Routing — automated by `create`:**

1. **Label into a namespace dir.** `requalify` repointed `//frameworks/libs/native_bridge_support/…`
   onto the lane; the finder drops namespace-declaring lane bps and keeps the stock parallel, so the
   module was "undefined" while its `.bp` sat on disk. **Directory existence is not routing.** Fixed
   in `requalifyPath`, mirroring the finder exactly — including the `/pods/` carve-out, since
   decomposition pods *stay* namespaced and loaded.
2. **Cross-tree qualified dep.** An **un-forked** consumer (`platform_testing`) holds
   `//frameworks/libs/systemui:view_capture_proto`; the fork moved the module into the lane
   namespace. Don't edit the stock consumer — un-fork the one directory. **Size it first: 25 of 26
   such refs were `__subpackages__`/`__pkg__` visibility specs, not deps.**
3. **`.proto` imports** → `runRelocateStockSourcePaths`. Both roots — an early revision matched only
   `frameworks/` and left 17 `packages/` imports.
4. **Generated-header includes** (`.pb.h`, `.proto.h`) → same pass, producer-aware predicate.
5. **Bare + embedded paths** → `requalify -paths` (safe tree-wide; existence-guarded).
6. **`aidl.include_dirs`** → same. Both roots on the aidl path makes a type resolve twice.
7. **Executable bit.** `copyFile` used `os.Create`, so **387 of 387** executables under `frameworks/`
   arrived non-executable — silent until a genrule tried to *run* one, ~7,800 steps in.

**Lane-created — automated by `create` (`laneToolFixes`):**

8. **`streaming_proto` hyphen.** Include guards are built from the file path; `make_constant_name`
   passes `-` through, so `frameworks-<lane>/` yields `ANDROID_FRAMEWORKS-<LANE>_…` — an **illegal C
   macro**. Stock never hits it: AOSP has no hyphenated top-level dirs. **The naming convention that
   makes lanes work is what breaks the generator.**
9. **`ProtoLogTool`** asserts a hardcoded stock path is among its inputs.

**Shared-tree allowlists — DISCOVERED and automated by `create`:**

10. **Lane allowlists outside `build/soong`.** Any Soong plugin anywhere in the tree may keep its own
    per-lane path list — `external/icu/build/icu.go` carries the `libandroidicu`/`libicuuc`
    `AddNeverAllowRules` allowlists, and a lane missing from them fails with *"violates neverallow
    requirements"*, an error naming neither the lane nor the file.

    A **fixed list of known sites would be a false promise** — the next plugin to add one would not be
    on it. `discoverLaneAllowlists` finds them from a property that is true by construction:

    > a `.go` file already containing a `"<root>-<existing-lane>/…"` literal **is** a lane allowlist,
    > and the new lane belongs beside that entry.

    Self-maintaining: a plugin added upstream tomorrow is picked up the moment any lane registers in
    it. Files the dedicated patchers already own (`finder.go`, `neverallow.go`, `visibility.go`,
    `aar.go`) are excluded — **ownership beats pattern-matching**, since those are full of lane
    literals by design and discovery would fight the purpose-built patch that understands their
    structure. Snapshot/archive dirs are skipped: patching one is harmless but reports a site that
    does not exist, which teaches a false map of the tree.

    Insertions are AST-verified — the file is parsed before and **re-parsed after**, and a patch that
    would not compile is refused rather than written. They stage like every other soong patch
    (preview-then-apply).

    On the live tree this finds **six** sites: `external/icu/build/icu.go`,
    `build/soong/aconfig/all_aconfig_declarations.go` (whose comment literally says *"Extend this list
    when a new lane subtree is added"*), `android/androidmk.go`, `android/apex.go`, `java/sdk.go`,
    `kotlin/sdk.go`. **The last two were never found by hand** — the manual grep that located the
    other four missed them, which is the case for automating discovery rather than curating a list.

**Stock defects the mature lane already fixed — manual, and each fix must be NEUTRAL:**

11. **`PhotopickerLib` `-Werror`** on a deprecated androidx API. Fix: `@Suppress("DEPRECATION")`.
12. **`<uses-library>` mismatch.** An `androidx.wear.*` prebuilt AAR declares `wear-sdk` that the bp
    never does. Fix: `optional_uses_libs: ["wear-sdk"]` — soong's own recommended remedy.
    **Sweep by predicate, not by error**: "lane `android_app` + depends on `androidx.wear.*`" named
    all three affected modules at once.
13. **Artifact-path requirement.** Apps landing in `system/app/` violate the GSI requirement;
    `PRODUCT_ENFORCE_ARTIFACT_PATH_REQUIREMENTS := relaxed` does **not** cover it. Add
    `PRODUCT_ARTIFACT_PATH_REQUIREMENT_ALLOWED_LIST += <path>%`.
14. **Stray `Android.bp` under the AOSP root.** A graveyard or snapshot left *inside* the tree makes
    soong report duplicate modules. Keep snapshots outside; dirs holding only `.go`/`.mk` are safe.
15. **Un-forked infra.** `proto_logging`, `libs/systemui/viewcapturelib` — see `defaultInfraExcludes`.
16. **Blueprint `parallelVisit` panic** — *not* caused by rewrite breadth. It was `requalifyEmbedded`
    reached with a **stock** `laneMap`, a map it was never designed for, corrupting interior segments
    (`//frameworks/base/packages/SystemUI` → `…/base/packages-<lane>/SystemUI`). Guarded at the
    function. Tree-wide `-paths` with the guard: 135 bp, **zero** corruption, no panic.

> ⚠️ **Keeping a control lane faithful.** If the lane exists to *measure* a transformation, every fix
> must be build-metadata or build-tooling only. On `holo2test` that meant taking soong's neutral
> `optional_uses_libs` remedy rather than porting `frameworks-holo`'s library swap, and leaving
> `ColorUtils.kt` stock rather than importing holo's relocation of colour authority. **Porting the
> mature lane's *answers* destroys the very delta the control exists to expose** — port its *compile
> fixes*, never its design decisions.

### Method that worked, and method that wasted cycles

- ⭐ **Diff against a mature lane before theorising.** It answers "should this be lane-pointed?" in one
  command. Every time it was consulted the answer was immediate; every time it was skipped, a fix was
  over-applied.
- ⭐ **Scope by predicate, then verify the scope.** The row-shape tells you where a defect *could* be,
  not where it *is*. Six of these needed a **narrower** scope than the pattern suggested — the aidl
  sweep was 3 of 31 candidates, the build-tool fix 1 of 236. But the exec-bit fix was **387 of 387**,
  because "the source bit was set" is a *fact*, not a heuristic. **Apply wholesale when the predicate
  is exact; scope narrowly when it is a guess.**
- ⭐ **Read the actual compile invocation.** The `-I` list named the culprit directly in two separate
  investigations that had otherwise stalled on hypotheses about cached artifacts.
- ⛔ **A count over one root is not a count over the lane.** A scan reporting hits from only
  `frameworks/` in a two-root tree is incomplete, not clean.

## Lessons from the android-17 revival

Grounded on 2026-09-02 against the live `android-17.0.0_r1` tree and the `build.*.ninja.d` files:

- **`build.<product>.ninja.d` is a Blueprint inventory, not a product dependency list.** Soong parses every `Android.bp` the finder sees regardless of lunch target, so the 15 `aosp_panther` and 15 `aosp_cf_x86_64_only_phone` dependency files are identical. Diffing them tells you what the *tree* has, which is still useful: diffing pristine 17 against the tree on disk shows exactly which files hand edits removed or added.
- **Never overwrite a project the target manifest ships.** The hand bring-up copied all of `hardware/google` from AOSP 15. Five of those directories (`apf`, `av`, `camera`, `gfxstream`, `interfaces`) are real 17 git projects; the copy left them hundreds of files off HEAD, and the 15 copies of `aemu` and the gfxstream guest code duplicated modules that 17 had moved into `external/mesa3d/src/gfxstream`. Disabling the "duplicate" Blueprint files to silence that then removed 17's own definitions. The fix is `git checkout` from the project's HEAD, not another copy from 15. Only directories absent from the target manifest (`gchips`, `graphics`, `pixel`, `pixel-sepolicy`, the `device/google/*` families) are revival payload.
- **A green `m nothing` proves graph coherence for the whole tree, not for the device.** The undefined-module errors above come from cuttlefish-only modules; they block every product because analysis is global.

## A lane can be blind to itself

Lane isolation has two mechanisms, and only one of them is the lunch combo:

1. **The `_<lane>` lunch suffix** selects which `apply<Lane>BpRoutes` runs. This is what makes a lane
   build *be* that lane.
2. **The `-<lane>` directory suffix** drives the `isOtherLaneBp*` predicates that decide which bp
   files are dropped. This is what makes lanes blind to *each other*.

Registering a new lane teaches the shared `isOtherLaneBp` its suffix so the existing lanes stop
seeing it. But that same predicate also guards `dropNonHoloLaneBps`, which runs **before** any
lane's own routes — so a newly registered lane would drop its own content and arrive at its route
function with nothing left. The build is then **green and silently pure stock**: no error names the
lane, and the lane appears to work while contributing nothing.

`create` therefore excludes the new lane from that guard in the same pass that teaches the
predicate. The ordering is not incidental — the exclusion must follow the cross-cut that creates
the hazard.

## Design notes

- **Self-contained & portable.** Zero external deps; the Blueprint parser is vendored (upstream can't be `go get`'d cleanly). Runs on any AOSP tree / Android version.
- **Reversible.** `apply` snapshots every file it touches; the mirror never clobbers a hand-edited lane file.
- **Verified, not assumed.** Every capability was hardened by a real build finding a real gap — the test suite encodes each. The failures worth designing against are the ones that surface *far from their cause*: a bad lane name appearing as a robolectric dependency error, a self-dropping lane appearing as a clean build.
- **Fail before writing.** Checks that can run before generation (the name guard) do, because the cost of discovering a naming problem after a multi-GB clone and a shared-soong patch is not symmetric with the cost of checking first.

Licensed Apache-2.0. The vendored `internal/blueprint/parser` retains its upstream Apache-2.0 headers.
