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

# How It Works

The design in one sentence: **ask the target tree, do not carry a list of answers.**

A device tree cut from one Android release carries idioms a newer release rejects. The obvious
approach is a patch set for each release pair. That approach rots, because it encodes what was
true when someone wrote it. This toolkit instead probes the tree it is seeding into and acts only
on what it finds.

## Probe, then act

Ten compatibility operations run after the trees are mirrored. Every one is gated on a question
asked of the target tree, and every one is idempotent, so re-running changes nothing.

Two examples of what "probe" means concretely:

- **Illegal compiler flags.** The list is not hardcoded. It is read out of the target's own
  `build/soong/cc/config/global.go`, parsed as Go source, so the tool always drops exactly what
  that release rejects.
- **Renamed protobuf options.** Applied only when the target's own defining `.proto` declares the
  new field name and no longer declares the old one.

The consequence is that a wiped tree, re-seeded from scratch, reproduces the same green result.
Nothing depends on a human remembering a step.

## Derived, never enumerated

The same principle governs placement. Two examples, both of which were bugs before they were
principles:

- **Where vendor makefiles go** is read from the device tree that includes them, not from a table
  of paths. When the glue was placed by assumption instead, `BOARD_PREBUILT_VENDORIMAGE` never
  loaded and the super image shipped without a vendor partition.
- **Where a vendor blob goes** is read from the files that consume it: the copy-files entries and
  the Blueprint `srcs` that name it. Three devices ship both a 32-bit and a 64-bit copy of the
  same library, and only the consumers say which goes where.

## AST, not regular expressions

Blueprint files are edited through the canonical Blueprint parser, vendored because upstream
cannot be fetched with `go get`. Soong's Go source is read with `go/ast`. Policy and makefiles are
line-scanned, which is appropriate for data files but never for source.

## The bundle

The device trees are data, not a checkout: about 7100 files, embedded in the binary by default.
`go:embed` carries file content and nothing else, so executable bits, symlinks and empty
directories travel in generated manifests beside it. See [[Bundle Distribution]].

## What comes from the factory image

Everything AOSP cannot provide: the vendor and kernel-module partitions, the kernel itself, the
firmware requirements. The kernel a device really runs is read from the image rather than assumed,
because assuming it was wrong once already.
