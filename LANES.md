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

# The lane toolkit

The original half of this repository: forking parallel "lane" builds of AOSP without forking the
whole tree. It shares its code with device revival (`create` serves both) and is documented here
on its own so that the front door can describe one thing. Device revival is in [DESIGN.md](DESIGN.md)
and the [wiki](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/wiki).

# sovereign-lane-surgeon

[![CI](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/actions/workflows/ci.yml/badge.svg)](https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/actions/workflows/ci.yml)


**Seed a sovereign customization "lane" into an Android (AOSP) source tree — a parallel, first-class product variant that builds like stock, without overlays and without forking the whole tree.**

A single self-contained Go binary. Zero external dependencies. `go build` and run.

**v0.4.0** — stock-seeded lanes proven to a green full image. A whole-root fork of `frameworks/` +
`packages/` (178,264 files) now reaches `m droid` with zero errors in every phase. Getting there
surfaced sixteen distinct blocker classes; the automatable ones are applied by `create`, and all of
them are written up in [What a stock-seeded lane hits](#what-a-stock-seeded-lane-hits-and-why) so the
next lane — or the next reader — does not rediscover them one build at a time.

```
git clone … && cd Sovereign_Lane_Surgeon
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

## undefined-deps

`undefined-deps -name <lane> -out <root> [-fail]` is the AST census of every dependency name the lane's Android.bp files reference that nothing will define once the finder has routed the lane: stock parallels of lane bps are excluded (the finder drops them), and so are the bps the route manifest drops. It exists because Soong reports "depends on undefined module" one or two at a time and each report costs a full analysis run; landing a lane whose delta came from another Android version (the Holo lane on android-17) surfaces a whole class at once: prebuilts the source tree carried outside the lane, modules a conflicted bp still names by an old name, libraries the lane renamed. Diagnostic by default; `-fail` makes it a gate.

## Symlinks and Kotlin twins on android-17 (learned 2026-09-05, Holo lane on android-17.0.0_r1)

**Symlinks are opaque to Soong's finder and must stay symlinks in a lane.** android-17 stock carries
1,807 symlinks under `frameworks/` and `packages/` (1,700 to directories; 1,412 of them in
`packages/apps/CellBroadcastReceiver`, the rest in `frameworks/native/{include,libs}`, Car,
CellBroadcastService, Wifi, Connectivity). Seven directory symlinks point at a directory that holds an
`Android.bp`: WindowManager Shell's `multivalentTestsForDevice`, `multivalentTestsForDeviceless` and
`multivalentScreenshotTestsForDevice` (all to the unsuffixed sibling), Launcher3's
`tests/multivalentTestsForDevice`, `packages/modules/Virtualization/apex -> build/apex`,
`packages/services/Telephony/ecc/proto`, and the ThemePicker/WallpaperPicker2 robolectric `module`
links. The finder does not descend a symlinked directory, which is the only reason stock does not
define those modules three times. `mirrorSubtree` recreates every symlink as a symlink (relative
targets stay valid). Any later step that copies into a lane — absorbing upstream additions, restoring
a shed path, merging a delta — must do the same: `rsync -a` without a trailing slash on the link, or
`cp -P`. A trailing-slash rsync of a symlinked directory dereferences it into a real copy, and the lane
then defines every module in it twice. Gate 12 on android-17 lost ten modules to exactly this.

**Kotlin twin directories are KSS's, not the lane's.** A `kotlin/` directory beside a stock `java/`
whose files have `.java` counterparts (frameworks/opt/vcard, the stats VTS tests) is a Kotlin Release
Sovereignty twin from the android-15 lane. It must not be carried into a fresh lane: with the java bp
removed, stock's java bp loads and collides with the twin's bp. KSS re-creates the twins when the
`-kotlin` release is installed on the target. Stock's own `kotlin/` directories (SystemUI's
`util/kotlin`, Music) and Holo's own Kotlin libraries under `libs/androidx/compose/*/src/main/kotlin`
are not twins and stay.

**Path mapping is root-anchored only.** `frameworks/base/packages/SystemUI` maps to
`frameworks-holo/base/packages/SystemUI`; a bare `packages/ -> packages-holo/` replace rewrites the
interior segment and creates a phantom `frameworks-holo/base/packages-holo/` tree that defines every
module a second time. requalify's `requalifyEmbedded` guard exists for this; scripts outside the
toolkit must use the same root-anchored mapping.
