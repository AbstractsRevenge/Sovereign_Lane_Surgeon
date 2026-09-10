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

package main

import "strings"

// lanenaming.go — ONE source of truth for "is this module lane-renamed, and what is its stock base?"
// after the rename model moved from PREFIX (<Camel><base>) to SUFFIX (apps <base><Camel>, libs
// <base>_<lower>). The rename pass (renamepass.go) produces the suffix form, but every other pass that
// classifies a module as lane-owned vs keep-name — keepname-offork, reexport, fork-shared-infra,
// laneRenameReverseMap, dep-ledger — used a bare strings.HasPrefix(name, camel) and would silently
// mis-classify a suffix-renamed module as keep-name. These helpers recognize BOTH the suffix model and
// the legacy prefix, so they are correct on a fresh suffix lane AND on a not-yet-converted prefix lane.

// laneRenamedBase extracts the stock base from a lane-renamed module name, or ("", false) for a keep-name
// / stock name. Recognition order: lib suffix <base>_<lower>, app suffix <base><Camel>, then legacy prefix
// <Camel><base>. camel is the CamelCase token (e.g. "Holo2"/"Nexus"), lower the lowercase token
// ("holo2"/"nexus"). Either may be "" to skip that form.
func laneRenamedBase(name, camel, lower string) (string, bool) {
	if lower != "" {
		suf := "_" + lower
		if strings.HasSuffix(name, suf) && len(name) > len(suf) {
			return name[:len(name)-len(suf)], true
		}
	}
	if camel != "" && strings.HasSuffix(name, camel) && len(name) > len(camel) {
		return name[:len(name)-len(camel)], true
	}
	if camel != "" && strings.HasPrefix(name, camel) && len(name) > len(camel) {
		return name[len(camel):], true // legacy prefix model
	}
	return "", false
}

// isLaneRenamed reports whether name is a lane-renamed module (suffix or legacy prefix).
func isLaneRenamed(name, camel, lower string) bool {
	_, ok := laneRenamedBase(name, camel, lower)
	return ok
}

// laneRenamedForms returns the module names a stock base could carry once lane-renamed: the app suffix
// <base><Camel>, the lib suffix <base>_<lower>, and the legacy prefix <Camel><base>. A caller that knows
// a base but not the module type checks which of these the lane actually declares.
func laneRenamedForms(base, camel, lower string) []string {
	forms := make([]string, 0, 3)
	if camel != "" {
		forms = append(forms, base+camel) // app suffix
	}
	if lower != "" {
		forms = append(forms, base+"_"+lower) // lib/filegroup suffix
	}
	if camel != "" {
		forms = append(forms, camel+base) // legacy prefix
	}
	return forms
}
