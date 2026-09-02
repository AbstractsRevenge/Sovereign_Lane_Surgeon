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

// Package bpflags removes exact string entries from every cflags / cppflags / conlyflags list in
// an Android.bp, at any nesting depth (arch:, target:, soong_config_variables: maps and
// `[...] + select(...)` concatenations). AST-safe via the vendored Blueprint parser, form-preserving
// via parser.Print, idempotent. Shared by the surgeon's target-compat pass (targetcompat.go) and the
// standalone cmd/bpdropcflag.
package bpflags

import (
	"bytes"

	parser "github.com/abstractsrevenge/sovereign_lane_surgeon/internal/blueprint/parser"
)

// Props are the properties whose lists are edited.
var Props = map[string]bool{"cflags": true, "cppflags": true, "conlyflags": true}

func dropFromExpr(e parser.Expression, drop map[string]bool) bool {
	changed := false
	switch v := e.(type) {
	case *parser.List:
		kept := v.Values[:0]
		for _, it := range v.Values {
			if s, ok := it.(*parser.String); ok && drop[s.Value] {
				changed = true
				continue
			}
			kept = append(kept, it)
		}
		v.Values = kept
	case *parser.Operator:
		if dropFromExpr(v.Args[0], drop) {
			changed = true
		}
		if dropFromExpr(v.Args[1], drop) {
			changed = true
		}
	case *parser.Select:
		for _, c := range v.Cases {
			if c != nil && dropFromExpr(c.Value, drop) {
				changed = true
			}
		}
		if v.Append != nil && dropFromExpr(v.Append, drop) {
			changed = true
		}
	}
	return changed
}

// walkProps descends every property; a flag prop gets its entries dropped, any Map is recursed so
// arch/target/soong_config_variables blocks are reached.
func walkProps(props []*parser.Property, drop map[string]bool) bool {
	changed := false
	for _, p := range props {
		if Props[p.Name] {
			if dropFromExpr(p.Value, drop) {
				changed = true
			}
			continue
		}
		if m, ok := p.Value.(*parser.Map); ok && walkProps(m.Properties, drop) {
			changed = true
		}
	}
	return changed
}

// Drop removes every flag in drop from content's flag lists. Returns the (possibly unchanged)
// content and whether anything changed; a parse or print failure is returned as an error and
// content is left as it was.
func Drop(content []byte, drop map[string]bool) ([]byte, bool, error) {
	file, errs := parser.Parse("", bytes.NewReader(content))
	if len(errs) > 0 {
		return content, false, errs[0]
	}
	changed := false
	for _, def := range file.Defs {
		if mod, ok := def.(*parser.Module); ok && walkProps(mod.Properties, drop) {
			changed = true
		}
	}
	if !changed {
		return content, false, nil
	}
	out, err := parser.Print(file)
	if err != nil {
		return content, false, err
	}
	return out, true, nil
}
