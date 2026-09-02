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
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- hand-built encoders (test-only): enough LZ4 legacy / cpio newc / boot headers to round-trip ---

// lz4LiteralBlock encodes data as literal-only LZ4 sequences (valid, just uncompressed).
func lz4LiteralBlock(data []byte) []byte {
	var out []byte
	for len(data) > 0 {
		n := len(data)
		if n > 255+15+1000 {
			n = 255 + 15 + 1000
		}
		chunk := data[:n]
		data = data[n:]
		if n < 15 {
			out = append(out, byte(n<<4))
		} else {
			out = append(out, 0xF0)
			r := n - 15
			for r >= 255 {
				out = append(out, 255)
				r -= 255
			}
			out = append(out, byte(r))
		}
		out = append(out, chunk...)
	}
	return out
}

func lz4LegacyFrame(blocks ...[]byte) []byte {
	out := binary.LittleEndian.AppendUint32(nil, lz4LegacyMagic)
	for _, b := range blocks {
		out = binary.LittleEndian.AppendUint32(out, uint32(len(b)))
		out = append(out, b...)
	}
	return out
}

func cpioNewc(files map[string][]byte) []byte {
	var out []byte
	hdr := func(name string, mode uint32, size int) {
		h := fmt.Sprintf("070701%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x", 1, mode, 0, 0, 1, 0, size, 0, 0, 0, 0, len(name)+1, 0)
		out = append(out, h...)
		out = append(out, name...)
		out = append(out, 0)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	for name, data := range files {
		hdr(name, 0100644, len(data))
		out = append(out, data...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	hdr("TRAILER!!!", 0, 0)
	return out
}

func padTo(b []byte, page int) []byte {
	for len(b)%page != 0 {
		b = append(b, 0)
	}
	return b
}

func fakeBootImg(kernel []byte) []byte {
	h := make([]byte, bootImgV3PageSize)
	copy(h, "ANDROID!")
	binary.LittleEndian.PutUint32(h[8:], uint32(len(kernel)))
	binary.LittleEndian.PutUint32(h[40:], 4)
	return append(h, padTo(append([]byte{}, kernel...), bootImgV3PageSize)...)
}

func fakeVendorBootV4(page uint32, ramdisk []byte, dtb []byte) []byte {
	h := make([]byte, 2128)
	copy(h, "VNDRBOOT")
	le := binary.LittleEndian
	le.PutUint32(h[8:], 4)
	le.PutUint32(h[12:], page)
	le.PutUint32(h[24:], uint32(len(ramdisk)))
	le.PutUint32(h[2096:], 2128)
	le.PutUint32(h[2100:], uint32(len(dtb)))
	entry := make([]byte, 108)
	le.PutUint32(entry[0:], uint32(len(ramdisk)))
	le.PutUint32(entry[4:], 0)
	le.PutUint32(entry[8:], 1)
	copy(entry[12:], "vendor_ramdisk00")
	le.PutUint32(h[2112:], uint32(len(entry)))
	le.PutUint32(h[2116:], 1)
	le.PutUint32(h[2120:], uint32(len(entry)))
	img := padTo(h, int(page))
	img = append(img, padTo(append([]byte{}, ramdisk...), int(page))...)
	img = append(img, padTo(append([]byte{}, dtb...), int(page))...)
	img = append(img, padTo(entry, int(page))...)
	return img
}

// --- decoders ---

// A block with a real back-reference (offset 4, length 12) plus a trailing literal-only sequence,
// wrapped in two legacy frames back to back. Exercises match overlap and frame concatenation.
func TestLZ4LegacyDecode(t *testing.T) {
	block := []byte{0x48} // 4 literals, match len 8+4
	block = append(block, "abcd"...)
	block = append(block, 4, 0) // offset 4
	block = append(block, 0x50) // 5 literals, end
	block = append(block, "12345"...)
	want := "abcdabcdabcdabcd12345"
	frame := append(lz4LegacyFrame(block), lz4LegacyFrame(lz4LiteralBlock([]byte("tail")))...)
	got, err := lz4LegacyDecode(frame)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want+"tail" {
		t.Errorf("got %q want %q", got, want+"tail")
	}
	// long literal run (>15, extension bytes)
	long := bytes.Repeat([]byte("x"), 700)
	got, err = lz4LegacyDecode(lz4LegacyFrame(lz4LiteralBlock(long)))
	if err != nil || !bytes.Equal(got, long) {
		t.Errorf("long literal run: err=%v len=%d", err, len(got))
	}
	if _, err := lz4LegacyDecode([]byte{1, 2, 3, 4, 5}); err == nil {
		t.Error("non-legacy input must be rejected")
	}
	if _, err := lz4LegacyDecode(lz4LegacyFrame([]byte{0x48, 'a', 'b', 'c', 'd', 9, 0})); err == nil {
		t.Error("match offset past output must be rejected")
	}
}

func TestCpioNewc(t *testing.T) {
	a := cpioNewc(map[string][]byte{"lib/modules/a.ko": []byte("KO-A"), "lib/modules/modules.load": []byte("a.ko\n")})
	ents, err := cpioNewcEntries(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 2 {
		t.Fatalf("got %d entries", len(ents))
	}
	seen := map[string]string{}
	for _, e := range ents {
		if !e.isRegular() {
			t.Errorf("%s not regular", e.Name)
		}
		seen[e.Name] = string(e.Data)
	}
	if seen["lib/modules/a.ko"] != "KO-A" || seen["lib/modules/modules.load"] != "a.ko\n" {
		t.Errorf("bad entries: %v", seen)
	}
	if _, err := cpioNewcEntries([]byte("070707garbage")); err == nil {
		t.Error("non-newc must be rejected")
	}
}

func TestBootImages(t *testing.T) {
	k := lz4LegacyFrame(lz4LiteralBlock([]byte("kernel-bytes")))
	got, err := bootImageKernel(fakeBootImg(k))
	if err != nil || !bytes.Equal(got, k) {
		t.Fatalf("boot kernel: err=%v got=%q", err, got)
	}
	rd := []byte("compressed-ramdisk-bytes")
	rds, err := vendorBootRamdisks(fakeVendorBootV4(2048, rd, []byte("dtb")))
	if err != nil || len(rds) != 1 || !bytes.Equal(rds[0].Data, rd) || rds[0].Name != "vendor_ramdisk00" {
		t.Fatalf("vendor ramdisks: err=%v %+v", err, rds)
	}
	if _, err := bootImageKernel([]byte("nope")); err == nil {
		t.Error("bad boot magic must be rejected")
	}
}

func TestBoardExtraRamdiskModules(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "device", "google", "gs201"), 0o755)
	os.WriteFile(filepath.Join(root, "device", "google", "gs201", "BoardConfig-common.mk"), []byte("# x\nBOARD_PREBUILT_VENDOR_KERNEL_RAMDISK_KERNEL_MODULES = fips140.ko other.ko\nBOARD_VENDOR_KERNEL_RAMDISK_KERNEL_MODULES_LOAD_EXTRA = $(foreach k,...)\n"), 0o644)
	got := boardExtraRamdiskModules(root, "gs201")
	if strings.Join(got, ",") != "fips140.ko,other.ko" {
		t.Errorf("extras = %v", got)
	}
	if boardExtraRamdiskModules(root, "nosuch") != nil || boardExtraRamdiskModules(root, "") != nil {
		t.Error("missing board must yield nil")
	}
	list, dropped := stripListEntries([]byte("fips140.ko\na.ko\nkernel/x/other.ko\nb.ko\n"), got)
	if string(list) != "a.ko\nb.ko\n" || strings.Join(dropped, ",") != "fips140.ko,other.ko" {
		t.Errorf("list=%q dropped=%v", list, dropped)
	}
}

func TestVermagicHelpers(t *testing.T) {
	ko := []byte("\x00license=GPL\x00vermagic=6.1.157-android14-11-ge4470993d947-ab15260412 SMP preempt\x00")
	vm := vermagicIn(ko)
	if vm != "6.1.157-android14-11-ge4470993d947-ab15260412" {
		t.Errorf("vermagic = %q", vm)
	}
	if kernelBuildID(vm) != "15260412" || kernelMajorMinor(vm) != "6.1" {
		t.Errorf("id=%q mm=%q", kernelBuildID(vm), kernelMajorMinor(vm))
	}
	gki := []byte("vermagic=6.1.157-android14-11-gbd2-ab14791245 SMP\x00")
	if id, votes := dominantBuildID([][]byte{ko, gki, ko}); id != "15260412" || votes["14791245"] != 1 {
		t.Errorf("dominant = %q votes=%v", id, votes)
	}
	rel := kernelReleaseFromImage(lz4LegacyFrame(lz4LiteralBlock([]byte("...Linux version 6.1.157-android14-11-gbd2-ab14791245 (build) #1 SMP\n"))))
	if rel != "6.1.157-android14-11-gbd2-ab14791245" {
		t.Errorf("release = %q", rel)
	}
	if !moduleIsSigned([]byte("elf...sig~Module signature appended~\n")) || moduleIsSigned(ko) {
		t.Error("signature trailer detection wrong")
	}
	if vermagicIn([]byte("nothing")) != "" {
		t.Error("absent vermagic must be empty")
	}
}

// End to end on synthetic inputs: release flag → target dir; boot.img → boot.img + Image.lz4;
// vendor_kernel_boot ramdisk → first-stage .ko + vendor_kernel_boot.modules.load; dlkm dir →
// vendor_dlkm lists + .ko + insmod cfg; build-id mismatch refused; second run is a no-clobber no-op.
func TestAssembleKernelPrebuilt(t *testing.T) {
	out := t.TempDir()
	flags := filepath.Join(out, "build", "release", "flag_values", "cp2a")
	os.MkdirAll(flags, 0o755)
	os.WriteFile(filepath.Join(flags, "RELEASE_KERNEL_PANTHER_DIR.textproto"), []byte("name: \"RELEASE_KERNEL_PANTHER_DIR\"\nvalue: {\n  string_value: \"device/google/pantah-kernels/6.1/26Q2-15260412\"\n}\n"), 0o644)
	fac := filepath.Join(t.TempDir(), "panther")
	os.MkdirAll(fac, 0o755)
	kernel := lz4LegacyFrame(lz4LiteralBlock([]byte("Image")))
	os.WriteFile(filepath.Join(fac, "boot.img"), fakeBootImg(kernel), 0o644)
	os.WriteFile(filepath.Join(fac, "dtbo.img"), []byte("DTBO"), 0o644)
	ko := []byte("elf\x00vermagic=6.1.157-android14-11-gabc-ab15260412 SMP\x00")
	signedKo := append(append([]byte{}, ko...), "~Module signature appended~\n"...)
	rd := cpioNewc(map[string][]byte{"lib/modules/first.ko": ko, "lib/modules/shared.ko": signedKo, "lib/modules/modules.load": []byte("first.ko\nshared.ko\n")})
	os.WriteFile(filepath.Join(fac, "vendor_kernel_boot.img"), fakeVendorBootV4(2048, lz4LegacyFrame(lz4LiteralBlock(rd)), []byte("DTB-blob")), 0o644)
	dlkm := filepath.Join(out, "vendor", "google_devices", "panther", "dlkm")
	os.MkdirAll(filepath.Join(dlkm, "lib", "modules"), 0o755)
	os.MkdirAll(filepath.Join(dlkm, "etc"), 0o755)
	os.WriteFile(filepath.Join(dlkm, "lib", "modules", "second.ko"), ko, 0o644)
	os.WriteFile(filepath.Join(dlkm, "lib", "modules", "shared.ko"), ko, 0o644) // unsigned twin of the ramdisk's signed copy
	os.WriteFile(filepath.Join(dlkm, "lib", "modules", "modules.load"), []byte("second.ko\n"), 0o644)
	os.WriteFile(filepath.Join(dlkm, "lib", "modules", "modules.blocklist"), []byte("blocklist x\n"), 0o644)
	os.WriteFile(filepath.Join(dlkm, "etc", "init.insmod.panther.cfg"), []byte("cfg"), 0o644)

	k, err := assembleKernelPrebuilt(out, "cp2a", "panther", fac, dlkm, "", []string{"shared.ko"})
	if err != nil {
		t.Fatal(err)
	}
	if k.Dir != "device/google/pantah-kernels/6.1/26Q2-15260412" || k.FirstStageKo != 2 || k.DlkmKo != 2 || k.Modules != 3 || k.VendorBuildID != "15260412" {
		t.Errorf("report %+v", k)
	}
	d := filepath.Join(out, k.Dir)
	for f, want := range map[string]string{
		"boot.img": "", "dtbo.img": "DTBO", "Image.lz4": string(kernel), "first.ko": string(ko), "second.ko": string(ko),
		"shared.ko":                       string(signedKo), // the signed ramdisk copy beat the unsigned vendor_dlkm copy
		"vendor_kernel_boot.modules.load": "first.ko\n",     // shared.ko omitted: the board injects it (its .ko still ships)
		"modules.load":                    "first.ko\n",     // same list under the name gs101 boards read (AOSP 15 ships both)
		"vendor_kernel_boot.dtb":          "DTB-blob",       // the dtb section, for the build's dtb.img rule
		"vendor_dlkm.modules.load":        "second.ko\n",
		"vendor_dlkm.modules.blocklist":   "blocklist x\n", "init.insmod.panther.cfg": "cfg",
	} {
		b, err := os.ReadFile(filepath.Join(d, f))
		if err != nil {
			t.Errorf("missing %s: %v", f, err)
			continue
		}
		if want != "" && string(b) != want {
			t.Errorf("%s = %q want %q", f, b, want)
		}
	}
	if k.RamdiskImage != "vendor_kernel_boot.img" || strings.Join(k.DTBFrom, ",") != "vendor_kernel_boot.img" {
		t.Errorf("sources: ramdisk=%s dtb=%v", k.RamdiskImage, k.DTBFrom)
	}
	if !strings.Contains(strings.Join(k.Notes, "\n"), "no system_dlkm contents") {
		t.Errorf("must report the missing system_dlkm: %v", k.Notes)
	}
	// no-clobber re-run
	k2, err := assembleKernelPrebuilt(out, "cp2a", "panther", fac, dlkm, "", []string{"shared.ko"})
	if err != nil || k2.Wrote != 0 || k2.Skipped != k.Wrote {
		t.Errorf("re-run should skip everything: %+v err=%v", k2, err)
	}
	// build-id mismatch is refused
	os.WriteFile(filepath.Join(flags, "RELEASE_KERNEL_CHEETAH_DIR.textproto"), []byte("string_value: \"device/google/pantah-kernels/6.1/25Q1-13202328\"\n"), 0o644)
	if _, err := assembleKernelPrebuilt(out, "cp2a", "cheetah", fac, dlkm, "", nil); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("mismatched build id must be refused, got %v", err)
	}
}

// gs101 (Pixel 6 family): no vendor_kernel_boot.img — the first-stage modules and the dtb live in
// vendor_boot.img, the board reads modules.load, and there is no system_dlkm partition.
func TestAssembleKernelPrebuiltGs101Layout(t *testing.T) {
	out := t.TempDir()
	flags := filepath.Join(out, "build", "release", "flag_values", "cp2a")
	os.MkdirAll(flags, 0o755)
	os.WriteFile(filepath.Join(flags, "RELEASE_KERNEL_ORIOLE_DIR.textproto"), []byte("string_value: \"device/google/raviole-kernels/6.1/26Q2-15260412\"\n"), 0o644)
	fac := filepath.Join(t.TempDir(), "oriole")
	os.MkdirAll(fac, 0o755)
	kernel := lz4LegacyFrame(lz4LiteralBlock([]byte("Image")))
	os.WriteFile(filepath.Join(fac, "boot.img"), fakeBootImg(kernel), 0o644)
	os.WriteFile(filepath.Join(fac, "dtbo.img"), []byte("DTBO"), 0o644)
	ko := []byte("elf\x00vermagic=6.1.157-android14-11-gabc-ab15260412 SMP\x00")
	rd := cpioNewc(map[string][]byte{"lib/modules/first.ko": ko, "lib/modules/modules.load": []byte("first.ko\nfips140.ko\n"), "lib/modules/fips140.ko": ko})
	os.WriteFile(filepath.Join(fac, "vendor_boot.img"), fakeVendorBootV4(2048, lz4LegacyFrame(lz4LiteralBlock(rd)), []byte("GS101-DTB")), 0o644)

	k, err := assembleKernelPrebuilt(out, "cp2a", "oriole", fac, "", "", []string{"fips140.ko"})
	if err != nil {
		t.Fatal(err)
	}
	if k.RamdiskImage != "vendor_boot.img" || strings.Join(k.DTBFrom, ",") != "vendor_boot.img" || k.FirstStageKo != 2 {
		t.Errorf("report %+v", k)
	}
	d := filepath.Join(out, k.Dir)
	for f, want := range map[string]string{"modules.load": "first.ko\n", "vendor_boot.dtb": "GS101-DTB", "fips140.ko": string(ko)} {
		b, err := os.ReadFile(filepath.Join(d, f))
		if err != nil || string(b) != want {
			t.Errorf("%s = %q (err %v) want %q", f, b, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(d, "vendor_kernel_boot.modules.load")); err == nil {
		t.Error("a vendor_boot-sourced list must not claim the vendor_kernel_boot name")
	}
}
