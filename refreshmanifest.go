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

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// refreshmanifest.go — the `refresh-manifest` subcommand: recompute an already-seeded lane's route-manifest
// AST-derived fields (stock_var_definer_bps) IN PLACE, without re-forking, while
// preserving the seed's curated dropped_namespace_decl_paths / kept_stock_bp_paths. This lets a keep-vs-drop
// or var-scope rule change take effect on a lane already on disk by regenerating just the manifest.
func cmdRefreshManifest(args []string) int {
	fs := flag.NewFlagSet("refresh-manifest", flag.ExitOnError)
	name := fs.String("name", "", "lane name (e.g. holo2)")
	out := fs.String("out", "", "AOSP root")
	_ = fs.Parse(args)
	if *name == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "refresh-manifest: -name and -out are required")
		return 2
	}
	path := filepath.Join(*out, "."+*name, *name+"_bp_route_manifest.json")
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "refresh-manifest:", err)
		return 1
	}
	var m routeManifest
	if err := json.Unmarshal(b, &m); err != nil {
		fmt.Fprintln(os.Stderr, "refresh-manifest: parse:", err)
		return 1
	}
	m.StockVarDefinerBps = stockVarDefinerBps(*name, *out)
	nb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "refresh-manifest: marshal:", err)
		return 1
	}
	if err := os.WriteFile(path, append(nb, '\n'), 0644); err != nil {
		fmt.Fprintln(os.Stderr, "refresh-manifest: write:", err)
		return 1
	}
	fmt.Printf("refresh-manifest: %s updated — %d var-definer bps\n",
		*name, len(m.StockVarDefinerBps))
	return 0
}
