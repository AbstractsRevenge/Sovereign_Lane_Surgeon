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
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// flashkit.go — `assemble-super`: the post-build step that makes an android-17 image of a
// prebuilt-vendor device flashable, and the flash procedure as a script next to it.
//
// WHY (measured 2026-09-02/03 on cheetah, Build Capture run 152554Z):
//   - android-17 assembles super.img in Soong (build/soong/filesystem/super_image.go), whose
//     partition list is filtered to the partitions Soong itself generated. The vendor and
//     vendor_dlkm images a Pixel AOSP build takes as PREBUILTS (BOARD_PREBUILT_VENDORIMAGE /
//     BOARD_PREBUILT_VENDOR_DLKMIMAGE, the self-extractor mechanism) are not generated modules,
//     so the built super.img held system, system_ext, product and system_dlkm only — lpdump
//     showed no vendor_a. android-15's Makefile packed prebuilt images into super; 17 does not.
//     A super without vendor boots nothing. This step runs the build's own build_super_image
//     with the build's own misc_info.txt plus the prebuilt images, so the result is what 15
//     would have produced.
//   - Three more facts the flash needs, each learned from a failed boot: vbmeta must go on
//     exactly as the build signed it (`fastboot --disable-verification` rewrites the flags in
//     flight, breaks the signature, and this bootloader then hands init no androidboot.vbmeta.*
//     — init dies in first stage); in bootloader mode `fastboot -w` only ERASES userdata and
//     metadata (the bootloader reports them as "raw"), so they are formatted as f2fs explicitly;
//     and the bootloader/baseband must be the ones the vendor blobs require (their
//     android-info.txt) — with older firmware userspace dies at ~65 s regardless of what is in
//     super. The generated script encodes all of it; the firmware step is left to the operator.

// superAssemblyPlan is what assemble-super will do, computed without running anything.
type superAssemblyPlan struct {
	MiscInfo   map[string]string // the rewritten dict for build_super_image
	Partitions []string          // final dynamic partition list
	Added      []string          // partitions supplied from prebuilt images
	Images     map[string]string // partition → image path
}

// parseMiscInfo reads key=value lines (the build's misc_info.txt form).
func parseMiscInfo(content []byte) (map[string]string, []string) {
	m := map[string]string{}
	var order []string
	for _, line := range strings.Split(string(content), "\n") {
		if i := strings.IndexByte(line, '='); i > 0 && !strings.HasPrefix(line, "#") {
			k := line[:i]
			if _, seen := m[k]; !seen {
				order = append(order, k)
			}
			m[k] = line[i+1:]
		}
	}
	return m, order
}

// planSuperAssembly builds the build_super_image dict: the build's super/dynamic-partition keys,
// every partition the build packed (its image under productOut), plus every partition in the
// board's list that the build left out but a prebuilt image supplies under prebuiltDir.
func planSuperAssembly(misc map[string]string, productOut, prebuiltDir string) (*superAssemblyPlan, error) {
	group := strings.TrimSpace(misc["super_partition_groups"])
	if group == "" || strings.Contains(group, " ") {
		return nil, fmt.Errorf("misc_info.txt: expected exactly one super partition group, got %q", group)
	}
	listKey := "super_" + group + "_partition_list"
	built := strings.Fields(misc[listKey])
	if len(built) == 0 {
		return nil, fmt.Errorf("misc_info.txt: %s is empty", listKey)
	}
	plan := &superAssemblyPlan{MiscInfo: map[string]string{}, Images: map[string]string{}}
	for k, v := range misc {
		switch {
		case k == "use_dynamic_partitions", k == "ab_update", k == "build_super_partition", k == "lpmake",
			strings.HasPrefix(k, "super_"), strings.HasPrefix(k, "virtual_ab"), strings.HasPrefix(k, "dynamic_partition"):
			plan.MiscInfo[k] = v
		}
	}
	parts := append([]string{}, built...)
	for _, p := range built {
		img := filepath.Join(productOut, p+".img")
		if !fileExists(img) {
			return nil, fmt.Errorf("built partition image missing: %s", img)
		}
		plan.Images[p] = img
	}
	// Partitions with a prebuilt image the build did not pack: vendor, vendor_dlkm, odm, odm_dlkm.
	for _, p := range []string{"vendor", "vendor_dlkm", "odm", "odm_dlkm"} {
		if _, have := plan.Images[p]; have {
			continue
		}
		img := filepath.Join(prebuiltDir, p+".img")
		if fileExists(img) {
			plan.Images[p] = img
			parts = append(parts, p)
			plan.Added = append(plan.Added, p)
		}
	}
	plan.Partitions = parts
	plan.MiscInfo[listKey] = strings.Join(parts, " ")
	plan.MiscInfo["dynamic_partition_list"] = strings.Join(parts, " ")
	for p, img := range plan.Images {
		plan.MiscInfo[p+"_image"] = img
	}
	return plan, nil
}

func (p *superAssemblyPlan) dictText() string {
	keys := make([]string, 0, len(p.MiscInfo))
	for k := range p.MiscInfo {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k + "=" + p.MiscInfo[k] + "\n")
	}
	return sb.String()
}

// flashScript renders the proven flash sequence for a device.
func flashScript(device, productOut, hostBin, prebuiltDir, superImg string, requirements []string) string {
	var sb strings.Builder
	sb.WriteString("#!/bin/bash\n# Flash " + device + " with the image sovereign-lane-surgeon assembled. Generated; read before running.\n")
	sb.WriteString("# Phone in fastboot (bootloader) mode, unlocked. Run from the AOSP root.\nset -e\n")
	sb.WriteString("P=" + productOut + "\nH=" + hostBin + "\nF=\"$H/fastboot\"\nV=" + prebuiltDir + "\n\n")
	sb.WriteString("# 1. Firmware the vendor blobs were built against (from the factory image's android-info.txt).\n")
	sb.WriteString("#    A vendor on older firmware dies in userspace ~65 s into boot with no log. If the phone\n#    reports older versions, flash the blobs' bootloader and radio first (as flash-all.sh does):\n")
	for _, r := range requirements {
		sb.WriteString("#      " + r + "\n")
	}
	sb.WriteString("#      $F flash bootloader $V/bootloader.img && $F reboot-bootloader && sleep 8\n#      $F flash radio $V/radio.img && $F reboot-bootloader && sleep 8\n")
	sb.WriteString("$F getvar version-bootloader; $F getvar version-baseband\n\n")
	sb.WriteString("# 2. Boot chain (the kernel and dtbo are the factory ones re-signed by the build).\nfor p in boot init_boot dtbo vendor_boot vendor_kernel_boot pvmfw; do [ -f \"$P/$p.img\" ] && $F flash $p \"$P/$p.img\"; done\n\n")
	sb.WriteString("# 3. vbmeta exactly as signed. NEVER --disable-verification: fastboot rewrites the flags in flight,\n#    the signature breaks, and this bootloader then passes init no androidboot.vbmeta.* at all.\n$F flash vbmeta \"$P/vbmeta.img\"\n$F flash vbmeta_system \"$P/vbmeta_system.img\"\n[ -f \"$V/vbmeta_vendor.img\" ] && $F flash vbmeta_vendor \"$V/vbmeta_vendor.img\"\n\n")
	sb.WriteString("# 4. The complete super (built partitions + prebuilt vendor/vendor_dlkm).\n$F flash super \"" + superImg + "\"\n\n")
	sb.WriteString("# 5. Wipe. In bootloader mode `fastboot -w` only erases (the bootloader reports userdata/metadata\n#    as raw) — format them explicitly, or the first boot dies on unformatted partitions.\n$F format:f2fs userdata\n$F format:f2fs metadata\n\n$F reboot\n")
	return sb.String()
}

// androidInfoRequirements returns the "require …" lines of a vendor android-info.txt.
func androidInfoRequirements(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "require ") {
			out = append(out, t)
		}
	}
	return out
}

func cmdAssembleSuper(args []string) int {
	fs := flag.NewFlagSet("assemble-super", flag.ExitOnError)
	device := fs.String("device", "", "device codename")
	out := fs.String("out", "", "AOSP root")
	buildOut := fs.String("build-out", "", "OUT_DIR of the finished build, relative to -out (default out-aosp17/<device>/eng)")
	_ = fs.Parse(args)
	if *device == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "assemble-super: -device and -out are required")
		return 2
	}
	if *buildOut == "" {
		*buildOut = filepath.Join("out-aosp17", *device, "eng")
	}
	productOut := filepath.Join(*out, *buildOut, "target", "product", *device)
	hostBin := filepath.Join(*out, *buildOut, "host", "linux-x86", "bin")
	prebuiltDir := filepath.Join(*out, "vendor", "google_devices", *device, "proprietary")
	miscBytes, err := os.ReadFile(filepath.Join(productOut, "misc_info.txt"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "assemble-super: %v (is the full build finished?)\n", err)
		return 1
	}
	misc, _ := parseMiscInfo(miscBytes)
	plan, err := planSuperAssembly(misc, productOut, prebuiltDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "assemble-super: %v\n", err)
		return 1
	}
	fmt.Printf("assemble-super %s: partitions %s\n", *device, strings.Join(plan.Partitions, " "))
	if len(plan.Added) == 0 {
		fmt.Println("  = the build's super already holds every partition; nothing to add")
	} else {
		fmt.Printf("  + from prebuilt images the build did not pack: %s\n", strings.Join(plan.Added, " "))
	}
	dict := filepath.Join(productOut, "super_full_misc_info.txt")
	if err := os.WriteFile(dict, []byte(plan.dictText()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "assemble-super:", err)
		return 1
	}
	superImg := filepath.Join(productOut, "super_full.img")
	tool := filepath.Join(hostBin, "build_super_image")
	cmd := exec.Command(tool, dict, superImg)
	cmd.Env = append(os.Environ(), "PATH="+hostBin+":"+os.Getenv("PATH"))
	if outb, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "assemble-super: %s failed: %v\n%s\n", tool, err, outb)
		return 1
	}
	fi, _ := os.Stat(superImg)
	fmt.Printf("  → %s (%d MB)\n", superImg, fi.Size()/1e6)
	reqs := androidInfoRequirements(filepath.Join(*out, "vendor", "google_devices", *device, "android-info.txt"))
	script := filepath.Join(productOut, "flash_"+*device+".sh")
	if err := os.WriteFile(script, []byte(flashScript(*device, productOut, hostBin, prebuiltDir, superImg, reqs)), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "assemble-super:", err)
		return 1
	}
	fmt.Printf("  → %s (the proven flash sequence; firmware step left to the operator)\n", script)
	for _, r := range reqs {
		fmt.Printf("  ~ blobs require: %s\n", r)
	}
	return 0
}
