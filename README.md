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
m -j16 nothing        # graph-coherence gate
m -j16 droid          # full image

# Changed your mind? Reverse the whole seed, byte-for-byte:
./sovereign-lane-surgeon uninstall -name myui -out /path/to/aosp
```

- **`create`** resolves the device family + SoC from the tree (e.g. `cheetah` → family `pantah`, SoC `gs201`), generates the device/goldfish/cuttlefish products, mirrors + requalifies the forked subtrees, and *stages* the in-place Soong patches for review (preview-then-apply — it never edits your `build/soong` until you `apply`).
- Add **`-rename`** for the branded, side-by-side model (curated forks).
- **Up to 3 lanes at once:** `-name a,b,c` — each is registered so it's blind to the others.

---

## Architecture

```
create ─┬─ soong patches   finder.go (per-lane BP routing), aar.go (isLaneLunch → framework
        │                   suppressors), visibility.go (lane↔stock pkg map)  [go/ast, AST-safe]
        ├─ device products  device/google/<family>-<lane>/aosp_<product>_<lane>.mk (+ SoC auto-fill)
        │                   goldfish + cuttlefish emulator products
        ├─ bp-mirror        clone forked subtrees (symlink-safe, .git-skipping)
        ├─ requalifier      //frameworks/base/…  →  //frameworks-<lane>/…   (fork-boundary aware,
        │                   AST-safe, form-preserving — vendored Blueprint parser)
        ├─ rename pass      (-rename only) installables (+overrides) + libs+dep-repoint through the
        │                   Blueprint AST; framework-class stays KEEP-NAME (Model-A hybrid)
        ├─ no-compose       (-no-compose only) leave Compose/AndroidX subtrees stock, auto-drop dangling
        │                   Compose dep refs, scope SystemUI srcs to the re-authored kotlin/** tree
        ├─ stock patch      compatibility.mk license-metadata back-fill (lane-independent) so a
        │                   full-image / compat-suite build doesn't fail "<tool> has no license metadata"
        └─ route manifest   drops the known stock danglers → builds ZERO-FLAG (see below)

apply     ── commits the staged soong patches, snapshotting each file first
uninstall ── fully reverses a seed (byte-identical): dirs + shared products + AndroidProducts.mk + soong
             patches (inverse AST edits — multi-lane-safe, leaves sibling lanes intact)
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

- ✅ **Keep-name sovereignty (zero-flag)** — proven end-to-end on a real tree: a from-scratch lane forking `frameworks/base` (~46k files) reaches a **zero-flag, zero-error `m nothing`** (product config + full Soong analysis + kati). Every generated artifact is verified against the hand-built reference lane it mirrors.
- ✅ **App-naming / rename (Model-A hybrid)** — installables + libs+dep-repoint AST-validated (round-trip clean on real SystemUI / `frameworks/base` / `services` blueprints); framework-class is keep-name (the stem/phony tier retired after it was proven to break `platform-bootclasspath`/`services`). Drove a real branded lane (Nexus-Modern, `frameworks-nexusm`/`packages-nexusm`, ~40 forked apps) to **zero-error `m nothing` Soong analysis**.
- ✅ **`reexport` idiom** — one pass over the Nexus-Modern lane detected 41 replaced-app stock subtrees and auto-generated 35 correct keep-name re-export stubs, replacing hand-authored graph-coherence stubs.
- ✅ **`-no-compose`** — AndroidX/Compose-bypass wiring (exclude-gen + auto drop-dep + SystemUI srcs-scope), unit-tested.
- ✅ **`uninstall`** — byte-identical seed reversal, proven end-to-end; multi-lane-safe.
- 🚧 **Full `m droid` compile + boot** — validates forked *content*, not just graph coherence.
- 🚧 **kati re-green + `reexport` edge cases** — the fork-everything campaign's flat-namespace tail (JNI / proto / FQ-label refs) + folding the live finder's app-naming routing (root-disposition / full-subtree-drop / apex-keep) back into the generation template.
- 🗺️ **Roadmap** — a `doctor` that auto-detects danglers from build evidence, richer device-fork auto-fill.

Self-contained, **46 tests, zero external dependencies**.

## Design notes

- **Self-contained & portable.** Zero external deps; the Blueprint parser is vendored (upstream can't be `go get`'d cleanly). Runs on any AOSP tree / Android version.
- **Reversible.** `apply` snapshots every file it touches; the mirror never clobbers a hand-edited lane file.
- **Verified, not assumed.** Every capability was hardened by a real build finding a real gap — the test suite encodes each.

Licensed Apache-2.0. The vendored `internal/blueprint/parser` retains its upstream Apache-2.0 headers.
