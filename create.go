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

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LaneConfig is the identity of a sovereign lane, derived from one prompt (the name).
type LaneConfig struct {
	Name        string // "holo"
	CamelCase   string // "Holo" — installable module prefix
	LibPrefix   string // "holo" — lowercase, for internal lib names (holo-<base>)
	DirSuffix   string // "-holo" — frameworks-holo, packages-holo, device/google/<dev>-holo
	KeepName    bool   // true = keep-name + finder replacement (Holo model); false = rename + overrides (NexusM model)
	Devices     []string
	Goldfish    bool
	Cuttlefish  bool
	Forks       []string // stock subtrees to clone into the lane tree (frameworks/..., packages/...)
	ForkExclude []string // subpaths to LEAVE stock inside a fork (namespace-complex, e.g. frameworks/base/packages/SystemUI)
	DirPrefix   string   // physical directory prefix when KeepName is false (e.g. NexusM)
	FromLane    string   // source LANE to fork from (e.g. "holo") instead of stock; "" = stock fork
	NoCompose   bool     // OPTIONAL experimental premise: bypass AndroidX/Compose (Nexus-Modern style —
	//              exclude/leave-stock the Compose/AndroidX/Jetpack subtrees, auto-drop their dep
	//              refs, and scope SystemUI-class srcs to the re-authored View-in-Kotlin kotlin/ tree).
}

func deriveLane(name string, keepName bool, devices []string, goldfish, cuttlefish bool, dirPrefix string) LaneConfig {
	name = strings.ToLower(strings.TrimSpace(name))
	camel := name
	if name != "" {
		camel = strings.ToUpper(name[:1]) + name[1:]
	}
	return LaneConfig{
		Name: name, CamelCase: camel, LibPrefix: name, DirSuffix: "-" + name,
		KeepName: keepName, Devices: devices, Goldfish: goldfish, Cuttlefish: cuttlefish,
		DirPrefix: dirPrefix,
	}
}

func prompt(r *bufio.Reader, q, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", q, def)
	} else {
		fmt.Printf("%s: ", q)
	}
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func promptYesNo(r *bufio.Reader, q string, def bool) bool {
	d := "y"
	if !def {
		d = "n"
	}
	ans := strings.ToLower(prompt(r, q+" (y/n)", d))
	return strings.HasPrefix(ans, "y")
}

// printCreateIntro prints the one-paragraph create overview (§23.1).
func printCreateIntro() {
	fmt.Print(`sovereign-lane-surgeon create — scaffold a new sovereign lane (§23.1)

This mirrors stock into a parallel lane tree that builds like stock, then wires the
soong routing + device/emu products so a lane lunch loads the lane tree the way a stock
lunch loads frameworks/ + packages/. With -out it generates files; without, it prints the plan.

`)
}

// gatherLaneConfigInteractive prompts for a lane config on a tty. Returns ok=false when
// stdin is not a terminal (piped/cron) so the caller can fall back to the static plan.
func gatherLaneConfigInteractive() (LaneConfig, bool) {

	fi, _ := os.Stdin.Stat()
	if (fi.Mode() & os.ModeCharDevice) == 0 {
		return LaneConfig{}, false
	}
	r := bufio.NewReader(os.Stdin)
	nameInput := prompt(r, "Lane name (lowercase, e.g. holo / myui)", "myui")
	fmt.Println("Identity model:\n  1) keep-name + finder replacement (Holo model — lane bp replaces stock's, keeps stock names)\n  2) rename + overrides/stem (NexusM model — distinct <Name><Lane> names, overrides:[stock])")
	keep := prompt(r, "Choose 1 or 2", "1") != "2"
	devs := strings.FieldsFunc(prompt(r, "Device(s) to scaffold, comma-separated (e.g. lynx,tangorpro)", "lynx"), func(c rune) bool { return c == ',' || c == ' ' })
	for i := range devs {
		devs[i] = strings.TrimSpace(devs[i])
	}
	g := promptYesNo(r, "Add goldfish emulator products (sdk_phone64/sdk_tablet)?", true)
	c := promptYesNo(r, "Add cuttlefish products (aosp_cf phone/tablet — device-faithful, CI target)?", true)
	dirPrefix := ""
	if !keep {
		dirPrefix = prompt(r, "Physical directory prefix (e.g. NexusM)? Leave empty for none", "")
	}
	noCompose := promptYesNo(r, "Bypass AndroidX/Compose? (experimental Nexus-Modern premise — leave Compose subtrees stock, auto-drop their deps, scope SystemUI srcs to a re-authored kotlin/ tree)", false)
	cfg := deriveLane(nameInput, keep, devs, g, c, dirPrefix)
	cfg.NoCompose = noCompose
	return cfg, true
}

func printScaffoldPlan(c LaneConfig) {
	model := "keep-name + finder replacement"
	if !c.KeepName {
		model = "rename identity apps + overrides (Model-A hybrid: keep-name framework-class)"
	}
	fmt.Printf("PLAN for lane %q  (model: %s)\n", c.Name, model)
	fmt.Printf("  identity:   installables=%s<Name>, libs=%s-<base>, dirs=frameworks%s / packages%s\n", c.CamelCase, c.LibPrefix, c.DirSuffix, c.DirSuffix)
	fmt.Println("  would create / patch:")
	fmt.Printf("   1. mirror stock Android.bp → frameworks%s/ + packages%s/ (bpedits, byte-exact; v2)\n", c.DirSuffix, c.DirSuffix)
	fmt.Println("   2. soong patches (v2): finder.go (is<Lane>Product + apply<Lane>BpRoutes + route manifest),")
	fmt.Println("      aar.go (isLaneLunch + dir-guarded shouldSuppressStock* for framework-class),")
	fmt.Println("      visibility.go (laneCanonicalPkgs), androidmk.go (keep-name preference hook)")
	if !c.KeepName {
		fmt.Println("      + per-installable overrides:[\"<stock>\"] (rename model — identity apps + libs renamed;")
		fmt.Println("        framework-class stays KEEP-NAME/Model-A hybrid: finder-drop + aar.go suppressors, NOT stem+phony)")
	}
	if c.NoCompose {
		fmt.Println("   +  -no-compose (experimental AndroidX/Compose bypass): with -out, leaves the Compose/AndroidX")
		fmt.Println("      subtrees stock (computed from the tree), auto-drops dangling Compose dep refs, and scopes")
		fmt.Println("      SystemUI-class srcs to a re-authored kotlin/** View-in-Kotlin tree (which is authored by hand).")
	}
	for _, d := range c.Devices {
		fmt.Printf("   3. device/google/%s%s/ : aosp_%s_%s.mk (PRODUCT_NAME suffix → is<Lane>Product; SOONG_CONFIG opt-in;\n", d, c.DirSuffix, d, c.Name)
		fmt.Printf("      PRODUCT_PACKAGES→lane installables) + AndroidProducts.mk (register + COMMON_LUNCH_CHOICES)\n")
	}
	if c.Goldfish {
		fmt.Printf("   4. goldfish emu: sdk_phone64_%s.mk + sdk_tablet_%s.mk (+ register in goldfish AndroidProducts.mk)\n", c.Name, c.Name)
	}
	if c.Cuttlefish {
		fmt.Printf("   5. cuttlefish: aosp_cf_x86_64_phone_%s + _tablet_%s (shared cuttlefish tree; launch --config=tablet)\n", c.Name, c.Name)
	}
	fmt.Printf("   6. verify: aosp_build_capture -lane %s -build-cmd 'm -jN nothing' → then `audit` to enumerate fork gaps\n", c.Name)
	fmt.Println("\n(Foundation build: config + plan only. Steps 1–5 file generation land in the next increment;\n the audit/verify + taxonomy are already functional — see `audit -h`.)")
}
