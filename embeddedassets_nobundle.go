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

//go:build nobundle

package main

import "io/fs"

// Built with -tags nobundle: no content in the binary (~5MB instead of ~510MB). bundle.go's
// resolver supplies it — -bundle-dir, $SLS_BUNDLE_DIR, the cache, or -bundle-url /
// $SLS_BUNDLE_URL — and verifies every file against the embedded manifest before use.
func builtinBundle() fs.FS { return nil }
