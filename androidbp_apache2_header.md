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

# Apache 2.0 header — the canonical form for this project

Every file authored **in this repository** carries both copyright lines, dated 2026, followed by
the Apache 2.0 block. Files taken **verbatim from AOSP or another upstream** keep their original
copyright and year unchanged: `assets/aosp15_device/**` (the mirrored device trees),
`assets/kernel_headers/<family>/**` (kernel UAPI headers), `assets/overlays/hardware/**`
(upstream Soong-conversion sources) and `internal/blueprint/**` (the vendored Blueprint parser).
Relicensing or re-dating those would misstate their provenance.

`licenses_test.go` enforces this: it walks every tracked file, applies those exclusions, and fails
the suite when an authored file is missing the header — the same way `docs_test.go` fails when the
documentation drifts from the code.

## Go, Blueprint (`.bp`) and C/C++ — `//`

```
// Copyright 2026 The Android Open Source Project
// Copyright 2026 Sovereign Lane Surgeon
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
# Copyright 2026 The Android Open Source Project
# Copyright 2026 Sovereign Lane Surgeon
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
```

## Generated manifests

`cmd/bundlemanifest` emits the `#` form at the top of every manifest it writes, so regeneration
never drops it.
