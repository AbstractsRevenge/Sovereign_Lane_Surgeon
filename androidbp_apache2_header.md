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

# Apache 2.0 header: the canonical form for this project

Every file authored **in this repository** carries the author's copyright line, the project line,
and the Apache 2.0 block. The copyright holder is a person, because a project name cannot hold
copyright; the project line identifies the work and where it lives.

Files taken **verbatim from AOSP or another upstream** keep their original copyright and year
unchanged and never carry the author's line: `assets/aosp15_device/**` (the mirrored device
trees), `assets/kernel_headers/<family>/**` (kernel UAPI headers), `assets/overlays/hardware/**`
(upstream Soong-conversion sources) and `internal/blueprint/**` (the vendored Blueprint parser).
Relicensing or re-dating those would misstate their provenance; adding the author's line to them
would claim work that is not his.

Binary assets, the screenshots under `wiki/images/` and anything else that cannot hold a comment,
are covered by the repository's `LICENSE` instead. They are excluded by extension, listed
explicitly in the test rather than skipped silently, so adding one is a deliberate act.

`licenses_test.go` enforces all of this: it walks every tracked file, applies those exclusions,
fails the suite when an authored file is missing the header, and fails it when an upstream file
carries the author's line.

## Go, Blueprint (`.bp`) and C/C++: `//`

```
// Copyright 2026 Terrance Leverette (AbstractsRevenge)
// Sovereign Lane Surgeon: https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
```

## Makefiles, manifests and other `#`-comment data files

```
# Copyright 2026 Terrance Leverette (AbstractsRevenge)
# Sovereign Lane Surgeon: https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
```

## Markdown

```
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
```

## Generated manifests

`cmd/bundlemanifest` emits the `#` form at the top of every manifest it writes, so regeneration
never drops it.
