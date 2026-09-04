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
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// kernelprebuilt.go — assemble the per-device kernel prebuilt directory (the thing the AOSP 15
// tree shipped as device/google/<family>-kernels/6.1/<build>/) from a fetched factory image.
//
// WHY THIS EXISTS (grounded 2026-09-02 on android-17.0.0_r1 + CP2A.260705.006 factory images):
//   - The target release names the directory it expects through a release flag:
//     build/release/flag_values/<release>/RELEASE_KERNEL_<DEVICE>_DIR.textproto, e.g. cp2a →
//     device/google/pantah-kernels/6.1/26Q2-15260412. That build is not published in AOSP (the
//     public pantah-kernels/6.1 project stops at android-15.0.0_r36 and main holds 25Q1/trunk),
//     so the embedded AOSP 15 bundle cannot supply it and the factory image is the only source.
//   - The factory image's vendor_kernel_boot.img and vendor_dlkm.img modules carry vermagic
//     "6.1.157-android14-11-…-ab15260412": the SAME build id the flag names. The assembly checks
//     that agreement and refuses to write a directory whose contents contradict its name.
//   - What the board reads from the directory (device/google/gs201/BoardConfig-common.mk +
//     device.mk, android-17.0.0_r1 copy): boot.img (BOARD_PREBUILT_BOOTIMAGE), dtbo.img, Image.lz4
//     (copied as `kernel`), *.ko (flat, KERNEL_MODULES wildcard), vendor_kernel_boot.modules.load,
//     vendor_dlkm.modules.load(+blocklist), system_dlkm.modules.load(+blocklist), and optionally
//     init.insmod.<device>.cfg. Every list is consumed through $(notdir …), so bare module names
//     — the form the factory image's modules.load files already use — are correct.
//   - Sources inside the factory image: boot.img → boot.img + Image.lz4 (its kernel payload);
//     dtbo.img verbatim; vendor_kernel_boot.img → LZ4-legacy-compressed cpio ramdisk holding
//     lib/modules/*.ko + modules.load (first-stage modules) — or, on a SoC without that
//     partition (gs101: Pixel 6/6 Pro/6a), vendor_boot.img's "dlkm" ramdisk fragment, which the
//     gs101 board reads as `modules.load` (BOARD_VENDOR_RAMDISK_FRAGMENTS := dlkm; "Starting from
//     6.1, use modules.load") where gs201/zuma read `vendor_kernel_boot.modules.load`; the shipped
//     AOSP 15 dirs carry both names with identical content, and so does this assembly. The
//     device-tree blob the build's dtb.img rule cats from BOARD_PREBUILT_DTBIMAGE_DIR/*.dtb comes
//     out of whichever vendor boot image carries a dtb section. vendor_dlkm.img (already extracted by
//     the v0.4.0 vendor extraction to vendor/google_devices/<device>/dlkm) → lib/modules/*.ko,
//     modules.load, modules.blocklist, etc/init.insmod.*.cfg; system_dlkm.img (extracted the same
//     way when available) → lib/modules/**/modules.load + *.ko.
//
//   - ONE IMAGE, TWO KERNEL BUILDS (measured on CP2A panther): the kernel itself is the GKI build
//     ("Linux version 6.1.157-android14-11-gbd23337e42e7-ab14791245" inside Image.lz4) while 202 of
//     the 204 first-stage modules and 268 of the 328 vendor_dlkm modules carry the Pixel vendor build
//     "-ab15260412", which is the build id the release flag's directory name ends in. So the identity
//     check keys on the DOMINANT vendor-module build id, never on the first module found, and the GKI
//     release is reported separately from the decompressed kernel.
//   - BOARD-INJECTED FIRST-STAGE MODULES: the SoC board config declares modules it adds to the
//     ramdisk itself (gs201/zuma/zumapro: BOARD_PREBUILT_VENDOR_KERNEL_RAMDISK_KERNEL_MODULES =
//     fips140.ko; gs101 names the variable BOARD_PREBUILT_VENDOR_RAMDISK_KERNEL_MODULES) and
//     appends them to whatever vendor_kernel_boot.modules.load lists. The factory ramdisk's own
//     modules.load — the real boot order — already contains them, so the shipped list must omit
//     them (AOSP 15's prebuilt dir does: 204 names, no fips140.ko) or kati defines the module's
//     depmod rule twice ("overriding commands for target …/fips140.ko", observed 2026-09-02).
//   - SAME NAME, DIFFERENT BYTES: a GKI module present in two partitions differs by exactly a
//     module signature — the ramdisk and system_dlkm copies end in "~Module signature appended~",
//     the vendor_dlkm copies do not. The flat directory holds one file per name (the build packs it
//     into every image that lists it), so the SIGNED copy wins, first-stage first on ties.
//
// All decoding is pure Go (lz4legacy.go, cpio.go, bootimg.go): no lz4 binary, no python module.
// No-clobber throughout, like every other mirror step.

// releaseKernelDir reads RELEASE_KERNEL_<DEVICE>_DIR for the given release from the target tree's
// release config. Returns the repo-relative directory, e.g. "device/google/pantah-kernels/6.1/26Q2-15260412".
func releaseKernelDir(outRoot, release, device string) (string, error) {
	p := filepath.Join(outRoot, "build", "release", "flag_values", release,
		"RELEASE_KERNEL_"+strings.ToUpper(device)+"_DIR.textproto")
	b, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("release %q does not declare a kernel dir for %s (%s): %w", release, device, p, err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "string_value:") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(t, "string_value:"))
		v = strings.Trim(v, `"`)
		if v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("%s: no string_value", p)
}

// vermagicIn scans arbitrary bytes (a .ko's modinfo, or a whole ramdisk) for the first
// "vermagic=<release> " string and returns <release>, e.g. "6.1.157-android14-11-ge447…-ab15260412".
func vermagicIn(b []byte) string {
	i := bytes.Index(b, []byte("vermagic="))
	if i < 0 {
		return ""
	}
	rest := b[i+len("vermagic="):]
	end := bytes.IndexAny(rest, " \x00\n")
	if end < 0 {
		end = len(rest)
	}
	return string(rest[:end])
}

// kernelBuildID extracts the Android build id from a vermagic ("…-ab15260412" → "15260412").
func kernelBuildID(vermagic string) string {
	i := strings.LastIndex(vermagic, "-ab")
	if i < 0 {
		return ""
	}
	id := vermagic[i+3:]
	for j, c := range id {
		if c < '0' || c > '9' {
			return id[:j]
		}
	}
	return id
}

// moduleIsSigned reports whether a .ko carries an appended kernel module signature.
func moduleIsSigned(ko []byte) bool {
	return bytes.HasSuffix(ko, []byte("~Module signature appended~\n"))
}

// dominantBuildID returns the most frequent "-ab<id>" among the given modules' vermagics — the
// Pixel vendor kernel build — and the vote counts (GKI-built modules in the same set carry the
// GKI build id and are the minority).
func dominantBuildID(mods [][]byte) (string, map[string]int) {
	votes := map[string]int{}
	for _, m := range mods {
		if id := kernelBuildID(vermagicIn(m)); id != "" {
			votes[id]++
		}
	}
	best := ""
	for id, n := range votes {
		if best == "" || n > votes[best] || (n == votes[best] && id < best) {
			best = id
		}
	}
	return best, votes
}

// kernelReleaseFromImage returns the "Linux version …" release string of a kernel payload,
// decompressing an LZ4-legacy Image.lz4 first (the literal may or may not survive compression).
func kernelReleaseFromImage(kernel []byte) string {
	raw := kernel
	if len(kernel) >= 4 && binary.LittleEndian.Uint32(kernel) == lz4LegacyMagic {
		if d, err := lz4LegacyDecode(kernel); err == nil {
			raw = d
		}
	}
	i := bytes.Index(raw, []byte("Linux version "))
	if i < 0 {
		return ""
	}
	rest := raw[i+len("Linux version "):]
	end := bytes.IndexAny(rest, " \x00\n")
	if end < 0 {
		end = len(rest)
	}
	return string(rest[:end])
}

// boardExtraRamdiskModules returns the modules the SoC board injects into the first-stage ramdisk
// itself, by string-scanning device/google/<soc>/BoardConfig-common.mk for the two variable
// spellings the Pixel boards use (no regex — HARD RULE 3). Missing file or variable → nil.
func boardExtraRamdiskModules(outRoot, soc string) []string {
	if soc == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(outRoot, "device", "google", soc, "BoardConfig-common.mk"))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		for _, v := range []string{"BOARD_PREBUILT_VENDOR_KERNEL_RAMDISK_KERNEL_MODULES", "BOARD_PREBUILT_VENDOR_RAMDISK_KERNEL_MODULES"} {
			if !strings.HasPrefix(t, v) {
				continue
			}
			rest := strings.TrimSpace(t[len(v):])
			rest = strings.TrimLeft(rest, ":?+")
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "="))
			for _, m := range strings.Fields(rest) {
				if strings.HasSuffix(m, ".ko") {
					out = append(out, m)
				}
			}
		}
	}
	return out
}

// kernelMajorMinor returns "6.1" from "6.1.157-…".
func kernelMajorMinor(vermagic string) string {
	parts := strings.SplitN(vermagic, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "." + parts[1]
}

// kernelAssembly is the report of one assembleKernelPrebuilt run.
type kernelAssembly struct {
	Dir           string   // repo-relative target dir
	RamdiskImage  string   // vendor_kernel_boot.img or vendor_boot.img — where the first-stage modules came from
	DTBFrom       []string // images whose dtb section was written as <image>.dtb
	Vermagic      string   // dominant vendor-module vermagic (Pixel vendor kernel build)
	KernelRelease string   // GKI kernel release from Image.lz4 ("Linux version …")
	VendorBuildID string
	Wrote         int
	Skipped       int // no-clobber
	FirstStageKo  int
	DlkmKo        int
	SystemKo      int
	Modules       int // distinct module names written (flat)
	Notes         []string
}

func (k *kernelAssembly) put(outRoot, rel string, data []byte) {
	abs := filepath.Join(outRoot, k.Dir, rel)
	if _, err := os.Stat(abs); err == nil {
		k.Skipped++
		return
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		k.Notes = append(k.Notes, "mkdir "+rel+": "+err.Error())
		return
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		k.Notes = append(k.Notes, "write "+rel+": "+err.Error())
		return
	}
	k.Wrote++
}

// assembleKernelPrebuilt builds outRoot/<RELEASE_KERNEL_<device>_DIR> from:
//
//	factoryDir  the extracted factory image dir for the device (boot.img, dtbo.img, vendor_kernel_boot.img)
//	dlkmDir     the extracted vendor_dlkm contents (…/dlkm, may be "" if not extracted)
//	sysDlkmDir  the extracted system_dlkm contents (…/system_dlkm, may be "" if not extracted)
func assembleKernelPrebuilt(outRoot, release, device, factoryDir, dlkmDir, sysDlkmDir string, boardExtra []string) (*kernelAssembly, error) {
	return assembleKernelPrebuiltFor(outRoot, release, device, "", factoryDir, dlkmDir, sysDlkmDir, boardExtra)
}

// assembleKernelPrebuiltFor is assembleKernelPrebuilt plus the device family, which selects the
// bundled kernel-headers (kernelheaders.go); "" skips them.
func assembleKernelPrebuiltFor(outRoot, release, device, family, factoryDir, dlkmDir, sysDlkmDir string, boardExtra []string) (*kernelAssembly, error) {
	dir, err := releaseKernelDir(outRoot, release, device)
	if err != nil {
		return nil, err
	}
	k := &kernelAssembly{Dir: dir}

	read := func(name string) ([]byte, error) { return os.ReadFile(filepath.Join(factoryDir, name)) }
	// First-stage modules: vendor_kernel_boot.img where the SoC has that partition, else the
	// vendor_boot.img ramdisk fragments (gs101).
	ramdiskImage := "vendor_kernel_boot.img"
	vkb, err := read(ramdiskImage)
	if err != nil {
		ramdiskImage = "vendor_boot.img"
		if vkb, err = read(ramdiskImage); err != nil {
			return nil, fmt.Errorf("neither vendor_kernel_boot.img nor vendor_boot.img in %s: %w", factoryDir, err)
		}
	}
	k.RamdiskImage = ramdiskImage
	ramdisks, err := vendorBootRamdisks(vkb)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ramdiskImage, err)
	}
	// first-stage modules + their load order, from the (lz4 legacy) cpio ramdisk(s)
	var firstStage []cpioEntry
	for _, rd := range ramdisks {
		raw, derr := lz4LegacyDecode(rd.Data)
		if derr != nil {
			return nil, fmt.Errorf("%s ramdisk %q: %w", ramdiskImage, rd.Name, derr)
		}
		ents, cerr := cpioNewcEntries(raw)
		if cerr != nil {
			return nil, fmt.Errorf("%s ramdisk %q: %w", ramdiskImage, rd.Name, cerr)
		}
		firstStage = append(firstStage, ents...)
	}
	var firstKo [][]byte
	for _, e := range firstStage {
		if e.isRegular() && strings.HasSuffix(e.Name, ".ko") {
			firstKo = append(firstKo, e.Data)
		}
	}
	id, votes := dominantBuildID(firstKo)
	if id == "" {
		return nil, fmt.Errorf("%s carries no module with a vermagic — cannot identify the kernel build", ramdiskImage)
	}
	k.VendorBuildID = id
	for _, m := range firstKo {
		if vm := vermagicIn(m); kernelBuildID(vm) == id {
			k.Vermagic = vm
			break
		}
	}
	// The directory's name (from the release flag) and its contents (from the factory image) must
	// agree on the vendor build id, or the tree would claim one kernel and ship another.
	if !strings.HasSuffix(dir, "-"+id) {
		return nil, fmt.Errorf("release %s expects %s but the factory image's vendor modules are build %s (%v): refusing to assemble a mislabeled kernel dir", release, dir, id, votes)
	}

	boot, err := read("boot.img")
	if err != nil {
		return nil, fmt.Errorf("boot.img: %w", err)
	}
	kernel, err := bootImageKernel(boot)
	if err != nil {
		return nil, err
	}
	k.KernelRelease = kernelReleaseFromImage(kernel)
	if k.KernelRelease == "" {
		k.Notes = append(k.Notes, "could not read the kernel release string out of boot.img's kernel payload")
	}
	k.put(outRoot, "boot.img", boot)
	k.put(outRoot, "Image.lz4", kernel)
	if dtbo, derr := read("dtbo.img"); derr == nil {
		k.put(outRoot, "dtbo.img", dtbo)
	} else {
		k.Notes = append(k.Notes, "dtbo.img not in factory dir (BOARD_PREBUILT_DTBOIMAGE will dangle)")
	}
	// Device-tree blob(s): the build cats BOARD_PREBUILT_DTBIMAGE_DIR/*.dtb into dtb.img, so the
	// section a vendor boot image carries is written as one .dtb named after its image.
	for _, img := range []string{"vendor_boot.img", "vendor_kernel_boot.img"} {
		b, rerr := read(img)
		if rerr != nil {
			continue
		}
		dtb, derr := vendorBootDTB(b)
		if derr != nil || len(dtb) == 0 {
			continue
		}
		k.put(outRoot, strings.TrimSuffix(img, ".img")+".dtb", dtb)
		k.DTBFrom = append(k.DTBFrom, img)
	}
	if len(k.DTBFrom) == 0 {
		k.Notes = append(k.Notes, "no vendor boot image carries a dtb section: dtb.img cannot be built from this dir")
	}

	// Modules from all three partitions, one flat file per name: signed copy wins, first-stage
	// wins ties (boot needs it earliest). Lists and configs are per-partition and never collide.
	type modSrc struct {
		data   []byte
		signed bool
		rank   int // 0 first-stage, 1 system_dlkm, 2 vendor_dlkm
	}
	mods := map[string]modSrc{}
	offer := func(name string, data []byte, rank int) {
		c := modSrc{data: data, signed: moduleIsSigned(data), rank: rank}
		if cur, ok := mods[name]; ok {
			if cur.signed && !c.signed {
				return
			}
			if cur.signed == c.signed && cur.rank <= c.rank {
				return
			}
		}
		mods[name] = c
	}
	for _, e := range firstStage {
		if !e.isRegular() {
			continue
		}
		base := filepath.Base(e.Name)
		switch {
		case strings.HasSuffix(base, ".ko"):
			offer(base, e.Data, 0)
			k.FirstStageKo++
		case base == "modules.load":
			list, dropped := stripListEntries(e.Data, boardExtra)
			if len(dropped) > 0 {
				k.Notes = append(k.Notes, "first-stage modules.load: omitted board-injected "+strings.Join(dropped, ", ")+" (the board appends them itself)")
			}
			// gs101 boards read modules.load; gs201/zuma boards read vendor_kernel_boot.modules.load.
			// AOSP 15's shipped dirs carry both names with identical content.
			k.put(outRoot, "modules.load", list)
			if ramdiskImage == "vendor_kernel_boot.img" {
				k.put(outRoot, "vendor_kernel_boot.modules.load", list)
			}
		}
	}
	if k.FirstStageKo == 0 {
		k.Notes = append(k.Notes, "no .ko found in the "+ramdiskImage+" ramdisk")
	}

	// system_dlkm: GKI modules under lib/modules[/<release>]; lists consumed by $(notdir).
	if sysDlkmDir != "" {
		var loadFile string
		filepath.Walk(filepath.Join(sysDlkmDir, "lib", "modules"), func(p string, fi os.FileInfo, e error) error {
			if e != nil || fi.IsDir() {
				return nil
			}
			data, ferr := os.ReadFile(p)
			if ferr != nil {
				return nil
			}
			switch base := filepath.Base(p); {
			case strings.HasSuffix(base, ".ko"):
				offer(base, data, 1)
				k.SystemKo++
			case base == "modules.load" && loadFile == "":
				loadFile = p
				k.put(outRoot, "system_dlkm.modules.load", data)
			case base == "modules.blocklist":
				k.put(outRoot, "system_dlkm.modules.blocklist", data)
			}
			return nil
		})
		if loadFile == "" {
			k.Notes = append(k.Notes, "system_dlkm has no modules.load")
		}
	} else {
		k.Notes = append(k.Notes, "no system_dlkm contents: system_dlkm.modules.load not written (gs201/zuma boards read it; gs101 has no such partition)")
	}

	// vendor_dlkm: every .ko (the load list names only the auto-loaded subset; the rest are loaded
	// by init.insmod.<device>.cfg and still ship), plus its lists and the insmod configs.
	if dlkmDir != "" {
		modsDir := filepath.Join(dlkmDir, "lib", "modules")
		if ents, rerr := os.ReadDir(modsDir); rerr == nil {
			for _, e := range ents {
				if e.IsDir() {
					continue
				}
				data, ferr := os.ReadFile(filepath.Join(modsDir, e.Name()))
				if ferr != nil {
					continue
				}
				switch {
				case strings.HasSuffix(e.Name(), ".ko"):
					offer(e.Name(), data, 2)
					k.DlkmKo++
				case e.Name() == "modules.load":
					k.put(outRoot, "vendor_dlkm.modules.load", data)
				case e.Name() == "modules.blocklist":
					k.put(outRoot, "vendor_dlkm.modules.blocklist", data)
				}
			}
		} else {
			k.Notes = append(k.Notes, "vendor_dlkm modules dir missing: "+modsDir)
		}
		cfgs, _ := filepath.Glob(filepath.Join(dlkmDir, "etc", "init.insmod.*.cfg"))
		sort.Strings(cfgs)
		for _, c := range cfgs {
			if data, ferr := os.ReadFile(c); ferr == nil {
				k.put(outRoot, filepath.Base(c), data)
			}
		}
	} else {
		k.Notes = append(k.Notes, "vendor_dlkm not extracted: vendor_dlkm.modules.load and its .ko are missing")
	}

	names := make([]string, 0, len(mods))
	for n := range mods {
		names = append(names, n)
	}
	sort.Strings(names)
	unsigned := 0
	for _, n := range names {
		k.put(outRoot, n, mods[n].data)
		if !mods[n].signed {
			unsigned++
		}
	}
	k.Modules = len(names)
	if family != "" {
		putKernelHeaders(k, outRoot, family)
	}
	if unsigned > 0 {
		k.Notes = append(k.Notes, fmt.Sprintf("%d of %d modules have no appended signature (only a vendor_dlkm copy exists for them)", unsigned, len(names)))
	}
	return k, nil
}

// stripListEntries removes every line whose base name is in drop from a modules.load list,
// preserving order and the remaining lines byte-for-byte. Returns the new list and what was dropped.
func stripListEntries(list []byte, drop []string) ([]byte, []string) {
	if len(drop) == 0 {
		return list, nil
	}
	set := map[string]bool{}
	for _, d := range drop {
		set[d] = true
	}
	var out []byte
	var dropped []string
	for _, line := range strings.SplitAfter(string(list), "\n") {
		name := filepath.Base(strings.TrimSpace(line))
		if set[name] {
			dropped = append(dropped, name)
			continue
		}
		out = append(out, line...)
	}
	return out, dropped
}

// cmdExtractVendor is the standalone form of the vendor-image extraction (no download, no mirror):
// `extract-vendor -device panther -out <aosp-root> -factory-images-root <dir>`.
func cmdExtractVendor(args []string) int {
	fs := flag.NewFlagSet("extract-vendor", flag.ExitOnError)
	device := fs.String("device", "", "device codename")
	out := fs.String("out", "", "AOSP root (writes vendor/google_devices/<device>/{proprietary,dlkm,system_dlkm})")
	root := fs.String("factory-images-root", "", "parent dir of per-device factory-image extraction dirs (<root>/<device>/…)")
	_ = fs.Parse(args)
	if *device == "" || *out == "" || *root == "" {
		fmt.Fprintln(os.Stderr, "extract-vendor: -device, -out and -factory-images-root are required")
		return 2
	}
	n, err := extractVendorImages(*device, filepath.Join(*root, *device), *out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "extract-vendor: %v\n", err)
		return 1
	}
	fmt.Printf("extract-vendor: %d image(s) extracted for %s\n", n, *device)
	return 0
}

// cmdAssembleKernel is the standalone entry: `assemble-kernel -device panther -release cp2a
// -out <aosp-root> -factory-images-root <dir>`; create -stock -release runs the same step.
func cmdAssembleKernel(args []string) int {
	fs := flag.NewFlagSet("assemble-kernel", flag.ExitOnError)
	device := fs.String("device", "", "device codename (panther, cheetah, lynx, tangorpro)")
	release := fs.String("release", "", "target release config (the lunch's middle token, e.g. cp2a) — selects RELEASE_KERNEL_<DEVICE>_DIR")
	out := fs.String("out", "", "AOSP root")
	root := fs.String("factory-images-root", "", "parent dir of per-device factory-image extraction dirs (<root>/<device>/…)")
	_ = fs.Parse(args)
	if *device == "" || *release == "" || *out == "" || *root == "" {
		fmt.Fprintln(os.Stderr, "assemble-kernel: -device, -release, -out and -factory-images-root are required")
		return 2
	}
	_, rc := runAssembleKernel(*out, *release, *device, filepath.Join(*root, *device))
	return rc
}

// runAssembleKernel resolves the extracted dlkm dirs the vendor extraction leaves under
// vendor/google_devices/<device>/ and prints the assembly report. Returns the assembly (nil on
// failure) and an exit code.
func runAssembleKernel(outRoot, release, device, factoryDir string) (*kernelAssembly, int) {
	vd := filepath.Join(outRoot, "vendor", "google_devices", device)
	dlkm, sys := "", ""
	if fi, err := os.Stat(filepath.Join(vd, "dlkm")); err == nil && fi.IsDir() {
		dlkm = filepath.Join(vd, "dlkm")
	}
	if fi, err := os.Stat(filepath.Join(vd, "system_dlkm")); err == nil && fi.IsDir() {
		sys = filepath.Join(vd, "system_dlkm")
	}
	fmt.Printf("\nkernel prebuilt (%s, release %s) from %s:\n", device, release, factoryDir)
	res := resolveDevice(outRoot, device)
	extra := boardExtraRamdiskModules(outRoot, res.SoC)
	k, err := assembleKernelPrebuiltFor(outRoot, release, device, res.Family, factoryDir, dlkm, sys, extra)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! %v\n", err)
		return nil, 1
	}
	fmt.Printf("  → %s\n  GKI kernel %s; vendor modules build ab%s; first stage from %s; dtb from %s\n", k.Dir, k.KernelRelease, k.VendorBuildID, k.RamdiskImage, strings.Join(k.DTBFrom, "+"))
	fmt.Printf("  wrote %d file(s), %d already present; %d distinct modules (first-stage %d, system_dlkm %d, vendor_dlkm %d)\n", k.Wrote, k.Skipped, k.Modules, k.FirstStageKo, k.SystemKo, k.DlkmKo)
	for _, n := range k.Notes {
		fmt.Printf("  ~ %s\n", n)
	}
	return k, 0
}
