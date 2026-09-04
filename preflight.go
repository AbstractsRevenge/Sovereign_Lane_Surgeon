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
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// preflight.go — `preflight`: what can be known about a built image set WITHOUT a phone.
//
// WHY: the boot proof exists for one SoC generation (cheetah, gs201). For a family with no test
// device the next best evidence is the built images measured against the factory image they were
// derived from — the same comparisons that explained each of cheetah's failed boots, made
// mechanical: the kernel the boot image carries is the factory's; the super holds every
// partition the factory's super layout has (a super without vendor was failure #1); every
// partition the factory's vbmeta verifies and this build produces is verified by this build's
// vbmeta (the unsigned/rewritten vbmeta was failure #2); the build's android-info.txt names the
// firmware the blobs require (older firmware was failure #3); and the vendor board glue is where
// the device tree includes it from (the cause of #1). Each check is PASS/FAIL/SKIP with the
// measured values; a FAIL is a reason not to flash, not a guess.
//
// The super metadata is parsed here (liblp's on-disk format; the build's lpdump refuses the
// sparse super.img the build writes) after a sparse-aware prefix read — pure Go, like the kernel
// assembler.

type preflightCheck struct {
	Name, Status, Detail string
}

func (c preflightCheck) String() string {
	return fmt.Sprintf("%-4s %-18s %s", c.Status, c.Name, c.Detail)
}

// ── sparse prefix ────────────────────────────────────────────────────────────────────────────

const sparseMagic = 0xed26ff3a

// readRawPrefix returns the first want bytes of an image as they would be on the block device,
// decoding Android sparse chunks (RAW / FILL / DONT_CARE) when the file is sparse.
func readRawPrefix(path string, want int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	hdr := make([]byte, 28)
	if _, err := f.ReadAt(hdr, 0); err != nil {
		return nil, err
	}
	if binary.LittleEndian.Uint32(hdr) != sparseMagic {
		out := make([]byte, want)
		n, rerr := f.ReadAt(out, 0)
		if rerr != nil && n < want {
			return out[:n], nil
		}
		return out, nil
	}
	fileHdr := int64(binary.LittleEndian.Uint16(hdr[8:]))
	chunkHdr := int64(binary.LittleEndian.Uint16(hdr[10:]))
	blk := int(binary.LittleEndian.Uint32(hdr[12:]))
	chunks := binary.LittleEndian.Uint32(hdr[20:])
	out := make([]byte, 0, want)
	off := fileHdr
	for i := uint32(0); i < chunks && len(out) < want; i++ {
		ch := make([]byte, chunkHdr)
		if _, err := f.ReadAt(ch, off); err != nil {
			return nil, fmt.Errorf("sparse chunk %d: %w", i, err)
		}
		typ := binary.LittleEndian.Uint16(ch)
		nblk := int(binary.LittleEndian.Uint32(ch[4:]))
		total := int64(binary.LittleEndian.Uint32(ch[8:]))
		size := nblk * blk
		switch typ {
		case 0xCAC1: // raw
			take := size
			if len(out)+take > want {
				take = want - len(out)
			}
			b := make([]byte, take)
			if _, err := f.ReadAt(b, off+chunkHdr); err != nil {
				return nil, err
			}
			out = append(out, b...)
		case 0xCAC2: // fill
			fill := make([]byte, 4)
			if _, err := f.ReadAt(fill, off+chunkHdr); err != nil {
				return nil, err
			}
			for j := 0; j < size && len(out) < want; j += 4 {
				out = append(out, fill...)
			}
		case 0xCAC3: // don't care
			for j := 0; j < size && len(out) < want; j += 4 {
				out = append(out, 0, 0, 0, 0)
			}
		}
		off += total
	}
	if len(out) > want {
		out = out[:want]
	}
	return out, nil
}

// ── liblp metadata ───────────────────────────────────────────────────────────────────────────

const (
	lpGeometryMagic  = 0x616c4467
	lpMetadataMagic  = 0x414C5030
	lpReservedBytes  = 4096
	lpGeometrySize   = 4096
	lpSectorBytes    = 512
	lpPartitionEntry = 52
	lpExtentEntry    = 24
	lpGroupEntry     = 48
)

type lpPartition struct {
	Name  string
	Group string
	Bytes int64 // sum of its extents
}

type lpGroup struct {
	Name    string
	MaxSize int64
}

type lpLayout struct {
	Partitions []lpPartition
	Groups     []lpGroup
}

func cstr36(b []byte) string { return cstr(b[:36]) }

// parseLPMetadata reads slot 0's primary metadata from a raw super prefix. Two on-disk layouts
// exist (liblp): a real super has 4096 reserved bytes, the geometry (and its backup), then the
// metadata slots; a metadata-only image (super_empty.img, what the factory ships) has the
// geometry at offset 0 and the metadata right after its 4096 bytes.
func parseLPMetadata(raw []byte) (*lpLayout, error) {
	var base int
	switch {
	case len(raw) >= lpGeometrySize && binary.LittleEndian.Uint32(raw) == lpGeometryMagic:
		base = lpGeometrySize
	case len(raw) >= lpReservedBytes+lpGeometrySize && binary.LittleEndian.Uint32(raw[lpReservedBytes:]) == lpGeometryMagic:
		base = lpReservedBytes + 2*lpGeometrySize
	default:
		return nil, fmt.Errorf("super: no logical-partition geometry at offset 0 or %d", lpReservedBytes)
	}
	if len(raw) < base+48 {
		return nil, fmt.Errorf("super: prefix too short for the metadata header (%d bytes)", len(raw))
	}
	h := raw[base:]
	if binary.LittleEndian.Uint32(h) != lpMetadataMagic {
		return nil, fmt.Errorf("super: no metadata header at offset %d", base)
	}
	headerSize := int(binary.LittleEndian.Uint32(h[8:]))
	tablesSize := int(binary.LittleEndian.Uint32(h[44:]))
	if len(h) < headerSize+tablesSize {
		return nil, fmt.Errorf("super: prefix too short for the metadata tables (%d < %d)", len(h), headerSize+tablesSize)
	}
	tables := h[headerSize : headerSize+tablesSize]
	desc := func(at int) (off, n, size int) {
		return int(binary.LittleEndian.Uint32(h[at:])), int(binary.LittleEndian.Uint32(h[at+4:])), int(binary.LittleEndian.Uint32(h[at+8:]))
	}
	pOff, pN, pSize := desc(80)
	eOff, eN, eSize := desc(92)
	gOff, gN, gSize := desc(104)
	if pSize != lpPartitionEntry || eSize != lpExtentEntry || gSize != lpGroupEntry {
		return nil, fmt.Errorf("super: unexpected table entry sizes %d/%d/%d", pSize, eSize, gSize)
	}
	lay := &lpLayout{}
	for i := 0; i < gN; i++ {
		e := tables[gOff+i*gSize:]
		lay.Groups = append(lay.Groups, lpGroup{Name: cstr36(e), MaxSize: int64(binary.LittleEndian.Uint64(e[40:]))})
	}
	extentBytes := func(first, n int) int64 {
		var sum int64
		for i := first; i < first+n && i < eN; i++ {
			e := tables[eOff+i*eSize:]
			sum += int64(binary.LittleEndian.Uint64(e)) * lpSectorBytes
		}
		return sum
	}
	for i := 0; i < pN; i++ {
		e := tables[pOff+i*pSize:]
		first := int(binary.LittleEndian.Uint32(e[40:]))
		n := int(binary.LittleEndian.Uint32(e[44:]))
		gi := int(binary.LittleEndian.Uint32(e[48:]))
		p := lpPartition{Name: cstr36(e), Bytes: extentBytes(first, n)}
		if gi < len(lay.Groups) {
			p.Group = lay.Groups[gi].Name
		}
		lay.Partitions = append(lay.Partitions, p)
	}
	return lay, nil
}

// superLayout reads a super image (sparse or raw) or a super_empty.img.
func superLayout(path string) (*lpLayout, error) {
	raw, err := readRawPrefix(path, 1<<20)
	if err != nil {
		return nil, err
	}
	return parseLPMetadata(raw)
}

func stripSlot(n string) string {
	return strings.TrimSuffix(strings.TrimSuffix(n, "_a"), "_b")
}

// ── avbtool ──────────────────────────────────────────────────────────────────────────────────

// runTool is exec'd for the host tools; replaced in tests.
var runTool = func(bin string, args ...string) ([]byte, error) {
	return exec.Command(bin, args...).CombinedOutput()
}

// parseAvbInfo returns the top-level flags value and every partition name a descriptor covers.
func parseAvbInfo(text string) (flags string, partitions []string) {
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(line, "Flags:") && flags == "" {
			flags = strings.TrimSpace(strings.TrimPrefix(t, "Flags:"))
		}
		if strings.HasPrefix(t, "Partition Name:") {
			n := strings.TrimSpace(strings.TrimPrefix(t, "Partition Name:"))
			if !seen[n] {
				seen[n] = true
				partitions = append(partitions, n)
			}
		}
	}
	sort.Strings(partitions)
	return flags, partitions
}

func avbPartitions(avbtool, image string) (string, []string, error) {
	out, err := runTool(avbtool, "info_image", "--image", image)
	if err != nil {
		return "", nil, fmt.Errorf("avbtool %s: %v", filepath.Base(image), err)
	}
	f, p := parseAvbInfo(string(out))
	return f, p, nil
}

// ── the checks ───────────────────────────────────────────────────────────────────────────────

func setOf(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func missingFrom(want []string, have map[string]bool) []string {
	var out []string
	for _, w := range want {
		if !have[w] {
			out = append(out, w)
		}
	}
	sort.Strings(out)
	return out
}

// preflightChecks runs every check; factoryDir may be "" (factory comparisons are then SKIP).
func preflightChecks(outRoot, device, productOut, hostBin, factoryDir string) []preflightCheck {
	var cs []preflightCheck
	add := func(name, status, detail string) { cs = append(cs, preflightCheck{name, status, detail}) }
	img := func(dir, name string) string { return filepath.Join(dir, name+".img") }

	// 1. images present — the boot chain the flash script flashes, plus super.
	required := []string{"boot", "dtbo", "vendor_boot", "vbmeta", "vbmeta_system"}
	for _, p := range []string{"init_boot", "vendor_kernel_boot", "pvmfw"} {
		if factoryDir == "" || fileExists(img(factoryDir, p)) {
			required = append(required, p)
		}
	}
	var missing []string
	for _, p := range required {
		if !fileExists(img(productOut, p)) {
			missing = append(missing, p)
		}
	}
	// super_full.img is assemble-super's fallback for a pre-fix tree; once the build's own
	// super.img is newer, that is the image the flash script uses and the one measured here.
	superImg := img(productOut, "super")
	if full := filepath.Join(productOut, "super_full.img"); fileExists(full) {
		fi, ferr := os.Stat(full)
		si, serr := os.Stat(superImg)
		if ferr == nil && (serr != nil || fi.ModTime().After(si.ModTime())) {
			superImg = full
		}
	}
	if !fileExists(superImg) {
		missing = append(missing, "super")
	}
	if len(missing) == 0 {
		add("images", "PASS", fmt.Sprintf("%s + %s", strings.Join(required, " "), filepath.Base(superImg)))
	} else {
		add("images", "FAIL", "missing: "+strings.Join(missing, " "))
	}

	// 2. kernel release: the boot image's kernel is the factory's.
	if factoryDir == "" {
		add("kernel", "SKIP", "no -factory-images-root")
	} else if bb, err := os.ReadFile(img(productOut, "boot")); err != nil {
		add("kernel", "FAIL", err.Error())
	} else if fb, err := os.ReadFile(img(factoryDir, "boot")); err != nil {
		add("kernel", "FAIL", err.Error())
	} else {
		rel := func(b []byte) string {
			k, err := bootImageKernel(b)
			if err != nil {
				return "?" + err.Error()
			}
			return kernelReleaseFromImage(k)
		}
		br, fr := rel(bb), rel(fb)
		if br != "" && br == fr {
			add("kernel", "PASS", br)
		} else {
			add("kernel", "FAIL", fmt.Sprintf("built %q, factory %q", br, fr))
		}
	}

	// 3–5. super: layout, coverage against the factory's super_empty, fit, populated.
	lay, err := superLayout(superImg)
	if err != nil {
		add("super", "FAIL", err.Error())
	} else {
		have := map[string]bool{}
		var names []string
		var empty []string
		for _, p := range lay.Partitions {
			n := stripSlot(p.Name)
			have[n] = true
			if strings.HasSuffix(p.Name, "_b") {
				continue // the inactive slot of an A/B super is empty by design
			}
			names = append(names, fmt.Sprintf("%s(%dM)", n, p.Bytes>>20))
			if p.Bytes == 0 {
				empty = append(empty, n)
			}
		}
		add("super-layout", "PASS", strings.Join(names, " "))
		if factoryDir == "" {
			add("super-coverage", "SKIP", "no -factory-images-root")
		} else if fl, err := superLayout(img(factoryDir, "super_empty")); err != nil {
			add("super-coverage", "FAIL", "factory super_empty: "+err.Error())
		} else {
			var want []string
			for _, p := range fl.Partitions {
				if strings.HasSuffix(p.Name, "_a") || !strings.Contains(p.Name, "_") || stripSlot(p.Name) == p.Name {
					want = append(want, stripSlot(p.Name))
				}
			}
			want = uniqueSorted(want)
			if m := missingFrom(want, have); len(m) == 0 {
				add("super-coverage", "PASS", "holds every partition of the factory layout: "+strings.Join(want, " "))
			} else {
				add("super-coverage", "FAIL", "factory layout partitions absent from the built super: "+strings.Join(m, " ")+" (a super without vendor boots nothing)")
			}
		}
		if len(empty) > 0 {
			add("super-populated", "FAIL", "partitions with no extents: "+strings.Join(empty, " "))
		} else {
			add("super-populated", "PASS", fmt.Sprintf("%d partitions carry data", len(lay.Partitions)))
		}
		fit := "PASS"
		var det []string
		for _, g := range lay.Groups {
			if g.MaxSize == 0 {
				continue
			}
			var sum int64
			for _, p := range lay.Partitions {
				if p.Group == g.Name {
					sum += p.Bytes
				}
			}
			det = append(det, fmt.Sprintf("%s %dM/%dM", g.Name, sum>>20, g.MaxSize>>20))
			if sum > g.MaxSize {
				fit = "FAIL"
			}
		}
		add("super-fit", fit, strings.Join(det, ", "))
	}

	// 6–7. vbmeta: everything the factory verifies that this build produces, this build verifies.
	avb := filepath.Join(hostBin, "avbtool")
	if flags, ours, err := avbPartitions(avb, img(productOut, "vbmeta")); err != nil {
		add("vbmeta", "FAIL", err.Error())
	} else {
		add("vbmeta-flags", "PASS", "flags "+flags+" (0 = verifying; 1 = hashtree disabled, the eng default; 2 = verification disabled)")
		if factoryDir == "" {
			add("vbmeta-coverage", "SKIP", "no -factory-images-root")
		} else if _, theirs, err := avbPartitions(avb, img(factoryDir, "vbmeta")); err != nil {
			add("vbmeta-coverage", "FAIL", err.Error())
		} else {
			var want []string
			for _, p := range theirs {
				if fileExists(img(productOut, p)) {
					want = append(want, p)
				}
			}
			if m := missingFrom(want, setOf(ours)); len(m) == 0 {
				add("vbmeta-coverage", "PASS", "verifies every factory-verified partition it builds: "+strings.Join(want, " "))
			} else {
				add("vbmeta-coverage", "FAIL", "built but not in this vbmeta: "+strings.Join(m, " ")+" (factory verifies them)")
			}
		}
	}

	// 8. android-info: the firmware requirements of the blobs.
	if factoryDir == "" {
		add("android-info", "SKIP", "no -factory-images-root")
	} else {
		want := androidInfoRequirements(filepath.Join(factoryDir, "android-info.txt"))
		have := setOf(androidInfoRequirements(filepath.Join(productOut, "android-info.txt")))
		var vers []string
		for _, w := range want {
			if strings.HasPrefix(w, "require version-") {
				vers = append(vers, w)
			}
		}
		if m := missingFrom(vers, have); len(m) == 0 && len(vers) > 0 {
			add("android-info", "PASS", strings.Join(vers, "; "))
		} else if len(vers) == 0 {
			add("android-info", "SKIP", "factory android-info.txt names no firmware versions")
		} else {
			add("android-info", "FAIL", "the build's android-info.txt lacks: "+strings.Join(m, "; ")+" (the vendor board glue did not reach the build)")
		}
	}

	// 9. vendor glue where the device tree includes it from.
	glue := filepath.Join(outRoot, "vendor", "google_devices", device, "proprietary", "BoardConfigVendor.mk")
	if fileExists(glue) {
		add("vendor-glue", "PASS", "proprietary/BoardConfigVendor.mk present (BOARD_PREBUILT_VENDORIMAGE reaches the build)")
	} else if fileExists(filepath.Join(outRoot, "vendor", "google_devices", device, "BoardConfigVendor.mk")) {
		add("vendor-glue", "FAIL", "BoardConfigVendor.mk is flat; the device tree includes proprietary/BoardConfigVendor.mk — re-run create -stock")
	} else {
		add("vendor-glue", "FAIL", "no BoardConfigVendor.mk under vendor/google_devices/"+device)
	}
	return cs
}

func uniqueSorted(xs []string) []string {
	m := setOf(xs)
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func cmdPreflight(args []string) int {
	fs := flag.NewFlagSet("preflight", flag.ExitOnError)
	device := fs.String("device", "", "device codename")
	out := fs.String("out", "", "AOSP root")
	buildOut := fs.String("build-out", "", "OUT_DIR of the finished build, relative to -out (default out-aosp17/<device>/eng)")
	factoryRoot := fs.String("factory-images-root", "", "parent dir of per-device factory extraction dirs (<root>/<device>/…) for the factory comparisons")
	_ = fs.Parse(args)
	if *device == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "preflight: -device and -out are required")
		return 2
	}
	if *buildOut == "" {
		*buildOut = filepath.Join("out-aosp17", *device, "eng")
	}
	productOut := filepath.Join(*out, *buildOut, "target", "product", *device)
	hostBin := filepath.Join(*out, *buildOut, "host", "linux-x86", "bin")
	factoryDir := ""
	if *factoryRoot != "" {
		factoryDir = filepath.Join(*factoryRoot, *device)
	}
	checks := preflightChecks(*out, *device, productOut, hostBin, factoryDir)
	rc := 0
	fmt.Printf("preflight %s (%s)\n", *device, productOut)
	for _, c := range checks {
		fmt.Println("  " + c.String())
		if c.Status == "FAIL" {
			rc = 1
		}
	}
	if rc == 0 {
		fmt.Println("VERDICT: FLASHABLE as far as the images can tell")
	} else {
		fmt.Println("VERDICT: DO NOT FLASH — fix the FAIL lines first")
	}
	return rc
}
