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
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// synthLP builds a raw super prefix with slot-0 metadata: groups and partitions with one extent each.
func synthLP(groups []lpGroup, parts []lpPartition) []byte {
	const maxSize = 65536
	raw := make([]byte, lpReservedBytes+2*lpGeometrySize+maxSize)
	g := raw[lpReservedBytes:]
	binary.LittleEndian.PutUint32(g, lpGeometryMagic)
	binary.LittleEndian.PutUint32(g[40:], maxSize)
	binary.LittleEndian.PutUint32(g[44:], 3)
	base := lpReservedBytes + 2*lpGeometrySize
	h := raw[base:]
	binary.LittleEndian.PutUint32(h, lpMetadataMagic)
	binary.LittleEndian.PutUint32(h[8:], 128) // header_size (v1.0)
	// tables: partitions, extents, groups
	pOff := 0
	eOff := pOff + len(parts)*lpPartitionEntry
	gOff := eOff + len(parts)*lpExtentEntry
	put := func(at, off, n, size int) {
		binary.LittleEndian.PutUint32(h[at:], uint32(off))
		binary.LittleEndian.PutUint32(h[at+4:], uint32(n))
		binary.LittleEndian.PutUint32(h[at+8:], uint32(size))
	}
	binary.LittleEndian.PutUint32(h[44:], uint32(len(parts)*(lpPartitionEntry+lpExtentEntry)+len(groups)*lpGroupEntry)) // tables_size
	put(80, pOff, len(parts), lpPartitionEntry)
	put(92, eOff, len(parts), lpExtentEntry)
	put(104, gOff, len(groups), lpGroupEntry)
	t := h[128:]
	gi := map[string]int{}
	for i, gr := range groups {
		e := t[gOff+i*lpGroupEntry:]
		copy(e, gr.Name)
		binary.LittleEndian.PutUint64(e[40:], uint64(gr.MaxSize))
		gi[gr.Name] = i
	}
	for i, p := range parts {
		e := t[pOff+i*lpPartitionEntry:]
		copy(e, p.Name)
		binary.LittleEndian.PutUint32(e[40:], uint32(i))
		n := uint32(1)
		if p.Bytes == 0 {
			n = 0
		}
		binary.LittleEndian.PutUint32(e[44:], n)
		binary.LittleEndian.PutUint32(e[48:], uint32(gi[p.Group]))
		x := t[eOff+i*lpExtentEntry:]
		binary.LittleEndian.PutUint64(x, uint64(p.Bytes/lpSectorBytes))
	}
	return raw
}

// The factory's super_empty.img: geometry at offset 0, metadata right after 4096 bytes, 5184
// bytes in all (cheetah CP2A). The same tables, relocated.
func TestParseLPMetadataMetadataOnlyImage(t *testing.T) {
	raw := synthLP([]lpGroup{{"default", 0}, {"g_a", 8 << 30}}, []lpPartition{{"vendor_a", "g_a", 1 << 20}, {"vendor_b", "g_a", 0}})
	base := lpReservedBytes + 2*lpGeometrySize
	tablesSize := int(binary.LittleEndian.Uint32(raw[base+44:]))
	only := append(append([]byte{}, raw[lpReservedBytes:lpReservedBytes+lpGeometrySize]...), raw[base:base+128+tablesSize]...)
	lay, err := parseLPMetadata(only)
	if err != nil {
		t.Fatal(err)
	}
	if len(lay.Partitions) != 2 || lay.Partitions[0].Name != "vendor_a" || lay.Groups[1].MaxSize != 8<<30 {
		t.Fatalf("%+v", lay)
	}
}

func TestParseLPMetadata(t *testing.T) {
	raw := synthLP([]lpGroup{{"default", 0}, {"google_dynamic_partitions_a", 8 << 30}},
		[]lpPartition{{"system_a", "google_dynamic_partitions_a", 900 << 20}, {"vendor_a", "google_dynamic_partitions_a", 700 << 20}, {"vendor_b", "google_dynamic_partitions_a", 0}})
	lay, err := parseLPMetadata(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(lay.Partitions) != 3 || lay.Partitions[1].Name != "vendor_a" || lay.Partitions[1].Bytes != 700<<20 || lay.Partitions[1].Group != "google_dynamic_partitions_a" {
		t.Fatalf("%+v", lay.Partitions)
	}
	if lay.Groups[1].MaxSize != 8<<30 {
		t.Fatalf("%+v", lay.Groups)
	}
}

// A sparse file whose first chunk is RAW carries the metadata; the reader must decode it.
func TestReadRawPrefixSparse(t *testing.T) {
	raw := synthLP([]lpGroup{{"g", 0}}, []lpPartition{{"vendor_a", "g", 4096}})
	blk := 4096
	// pad raw to a block multiple
	for len(raw)%blk != 0 {
		raw = append(raw, 0)
	}
	hdr := make([]byte, 28)
	binary.LittleEndian.PutUint32(hdr, sparseMagic)
	binary.LittleEndian.PutUint16(hdr[4:], 1)
	binary.LittleEndian.PutUint16(hdr[8:], 28)
	binary.LittleEndian.PutUint16(hdr[10:], 12)
	binary.LittleEndian.PutUint32(hdr[12:], uint32(blk))
	binary.LittleEndian.PutUint32(hdr[16:], uint32(len(raw)/blk+10))
	binary.LittleEndian.PutUint32(hdr[20:], 2)
	ch := make([]byte, 12)
	binary.LittleEndian.PutUint16(ch, 0xCAC1)
	binary.LittleEndian.PutUint32(ch[4:], uint32(len(raw)/blk))
	binary.LittleEndian.PutUint32(ch[8:], uint32(12+len(raw)))
	dc := make([]byte, 12)
	binary.LittleEndian.PutUint16(dc, 0xCAC3)
	binary.LittleEndian.PutUint32(dc[4:], 10)
	binary.LittleEndian.PutUint32(dc[8:], 12)
	p := filepath.Join(t.TempDir(), "super.img")
	var file []byte
	file = append(file, hdr...)
	file = append(file, ch...)
	file = append(file, raw...)
	file = append(file, dc...)
	if err := os.WriteFile(p, file, 0o644); err != nil {
		t.Fatal(err)
	}
	lay, err := superLayout(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(lay.Partitions) != 1 || lay.Partitions[0].Name != "vendor_a" {
		t.Fatalf("%+v", lay)
	}
}

func TestParseAvbInfo(t *testing.T) {
	text := "Minimum libavb version:   1.0\nFlags:                    1\nDescriptors:\n    Chain Partition descriptor:\n      Partition Name:          boot\n      Flags:                   0\n    Hash descriptor:\n      Partition Name:        dtbo\n    Hashtree descriptor:\n      Partition Name:        vendor\n"
	flags, parts := parseAvbInfo(text)
	if flags != "1" || strings.Join(parts, ",") != "boot,dtbo,vendor" {
		t.Fatalf("%q %v", flags, parts)
	}
}

// End to end on synthetic images: a super without vendor and a vbmeta without vbmeta_vendor
// (the two cheetah failures) are both FAIL; a matching set is PASS.
func TestPreflightChecksOnSyntheticImages(t *testing.T) {
	root := t.TempDir()
	device := "dev"
	productOut := filepath.Join(root, "out", "target", "product", device)
	factory := filepath.Join(root, "factory", device)
	hostBin := filepath.Join(root, "out", "host", "bin")
	kernel := append([]byte("ANDROID!"), make([]byte, 4096*2)...)
	binary.LittleEndian.PutUint32(kernel[8:], 64) // kernel_size
	binary.LittleEndian.PutUint32(kernel[40:], 4) // header version
	copy(kernel[4096:], "Linux version 6.1.157-android14-11-g1 (build)\x00")
	for _, dir := range []string{productOut, factory} {
		for _, p := range []string{"boot", "dtbo", "vendor_boot", "vbmeta", "vbmeta_system", "init_boot", "vendor_kernel_boot", "pvmfw", "vbmeta_vendor"} {
			write(t, filepath.Join(dir, p+".img"), "x")
		}
		if err := os.WriteFile(filepath.Join(dir, "boot.img"), kernel, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "android-info.txt"), []byte("require board=dev\nrequire version-bootloader=x-17\nrequire version-baseband=g-1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g := []lpGroup{{"default", 0}, {"google_dynamic_partitions_a", 8 << 30}}
	full := []lpPartition{{"system_a", "google_dynamic_partitions_a", 1 << 20}, {"vendor_a", "google_dynamic_partitions_a", 1 << 20}, {"vendor_dlkm_a", "google_dynamic_partitions_a", 1 << 20}}
	if err := os.WriteFile(filepath.Join(factory, "super_empty.img"), synthLP(g, full), 0o644); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "vendor", "google_devices", device, "proprietary", "BoardConfigVendor.mk"), "x")

	avbOut := map[string]string{}
	runTool = func(bin string, args ...string) ([]byte, error) {
		key := "out"
		if strings.Contains(args[2], string(filepath.Separator)+"factory"+string(filepath.Separator)) {
			key = "factory"
		}
		return []byte(avbOut[filepath.Base(args[2])+"@"+key]), nil
	}
	defer func() { runTool = nil }()
	factoryAvb := "Flags: 0\n" + strings.Join([]string{"boot", "init_boot", "vbmeta_system", "vbmeta_vendor", "abl", "dtbo", "vendor_boot", "vendor_kernel_boot", "pvmfw"}, "\n") + "\n"
	factoryAvb = strings.ReplaceAll(factoryAvb, "\n", "\n      Partition Name: ")
	avbOut["vbmeta.img@factory"] = factoryAvb

	// broken: super without vendor, vbmeta without vbmeta_vendor
	if err := os.WriteFile(filepath.Join(productOut, "super.img"), synthLP(g, full[:1]), 0o644); err != nil {
		t.Fatal(err)
	}
	avbOut["vbmeta.img@out"] = "Flags: 1\n      Partition Name: boot\n      Partition Name: init_boot\n      Partition Name: vbmeta_system\n      Partition Name: dtbo\n      Partition Name: vendor_boot\n      Partition Name: vendor_kernel_boot\n      Partition Name: pvmfw\n"
	status := map[string]string{}
	for _, c := range preflightChecks(root, device, productOut, hostBin, factory) {
		status[c.Name] = c.Status
		t.Log(c)
	}
	if status["super-coverage"] != "FAIL" || status["vbmeta-coverage"] != "FAIL" {
		t.Fatalf("expected the two cheetah failures to FAIL: %v", status)
	}
	for _, n := range []string{"images", "kernel", "super-fit", "android-info", "vendor-glue"} {
		if status[n] != "PASS" {
			t.Errorf("%s: %s", n, status[n])
		}
	}
	// fixed
	if err := os.WriteFile(filepath.Join(productOut, "super.img"), synthLP(g, full), 0o644); err != nil {
		t.Fatal(err)
	}
	avbOut["vbmeta.img@out"] += "      Partition Name: vbmeta_vendor\n"
	for _, c := range preflightChecks(root, device, productOut, hostBin, factory) {
		if c.Status == "FAIL" {
			t.Errorf("unexpected FAIL: %s", c)
		}
	}
}
