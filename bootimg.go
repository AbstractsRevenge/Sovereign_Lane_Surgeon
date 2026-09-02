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
	"fmt"
)

// bootimg.go — readers for the two Android boot image formats a factory image carries, laid out
// exactly as system/tools/mkbootimg/unpack_bootimg.py reads them (offsets were taken from that
// tool in the android-17.0.0_r1 tree, not from memory):
//
//	boot.img v3/v4:      "ANDROID!" | kernel_size @8 | ramdisk_size @12 | ... | header_version @40
//	                     page size fixed 4096; kernel occupies the pages right after the header page.
//	vendor_boot v3/v4:   "VNDRBOOT" | header_version @8 | page_size @12 | kernel_addr | ramdisk_addr |
//	                     vendor_ramdisk_size @24 | cmdline[2048] | tags_addr | name[16] | header_size |
//	                     dtb_size | dtb_addr(u64) | v4: table_size, entry_num, entry_size, bootconfig_size.
//	                     Sections follow page-aligned: ramdisk(s), dtb, ramdisk table, bootconfig.
//	                     Each v4 table entry: ramdisk_size, ramdisk_offset, ramdisk_type, name[32], board_id[16]u32.

const bootImgV3PageSize = 4096

func pages(n, page uint32) uint32 { return (n + page - 1) / page }

// bootImageKernel returns the raw kernel payload of a v3/v4 boot.img (for Pixel this is the
// LZ4-compressed Image, i.e. the file the device tree ships as Image.lz4).
func bootImageKernel(img []byte) ([]byte, error) {
	if len(img) < 44 || string(img[:8]) != "ANDROID!" {
		return nil, fmt.Errorf("boot.img: bad magic")
	}
	ver := binary.LittleEndian.Uint32(img[40:])
	if ver < 3 {
		return nil, fmt.Errorf("boot.img: header version %d not supported (need v3/v4)", ver)
	}
	ksize := binary.LittleEndian.Uint32(img[8:])
	start := uint32(bootImgV3PageSize)
	if int(start+ksize) > len(img) {
		return nil, fmt.Errorf("boot.img: kernel_size %d exceeds image", ksize)
	}
	return img[start : start+ksize], nil
}

// vendorRamdisk is one entry of a vendor_boot's ramdisk table (v3 images yield a single unnamed one).
type vendorRamdisk struct {
	Name string
	Data []byte
}

// vendorBootRamdisks returns every vendor ramdisk section of a v3/v4 vendor_boot / vendor_kernel_boot
// image, still compressed exactly as stored.
func vendorBootRamdisks(img []byte) ([]vendorRamdisk, error) {
	if len(img) < 2112 || string(img[:8]) != "VNDRBOOT" {
		return nil, fmt.Errorf("vendor_boot: bad magic")
	}
	le := binary.LittleEndian
	ver := le.Uint32(img[8:])
	page := le.Uint32(img[12:])
	rdTotal := le.Uint32(img[24:])
	hdrSize := le.Uint32(img[2096:])
	dtbSize := le.Uint32(img[2100:])
	if page == 0 {
		return nil, fmt.Errorf("vendor_boot: zero page size")
	}
	rdBase := page * pages(hdrSize, page)
	if int(rdBase+rdTotal) > len(img) {
		return nil, fmt.Errorf("vendor_boot: ramdisk section exceeds image")
	}
	if ver < 4 {
		return []vendorRamdisk{{Name: "vendor_ramdisk", Data: img[rdBase : rdBase+rdTotal]}}, nil
	}
	tableSize := le.Uint32(img[2112:])
	entryNum := le.Uint32(img[2116:])
	entrySize := le.Uint32(img[2120:])
	tableOff := page * (pages(hdrSize, page) + pages(rdTotal, page) + pages(dtbSize, page))
	if entrySize < 108 || int(tableOff+tableSize) > len(img) || entryNum*entrySize > tableSize {
		return nil, fmt.Errorf("vendor_boot: bad ramdisk table (off %d size %d entries %d x %d)", tableOff, tableSize, entryNum, entrySize)
	}
	var out []vendorRamdisk
	for i := uint32(0); i < entryNum; i++ {
		e := img[tableOff+i*entrySize:]
		size := le.Uint32(e[0:])
		off := le.Uint32(e[4:])
		name := cstr(e[12:44])
		s := rdBase + off
		if int(s+size) > len(img) {
			return nil, fmt.Errorf("vendor_boot: ramdisk %q exceeds image", name)
		}
		out = append(out, vendorRamdisk{Name: name, Data: img[s : s+size]})
	}
	return out, nil
}

// vendorBootDTB returns the device-tree blob section of a v3/v4 vendor_boot / vendor_kernel_boot
// image (the concatenated .dtb files the build's dtb.img rule would otherwise cat together from
// BOARD_PREBUILT_DTBIMAGE_DIR/*.dtb), or nil when the image carries none.
func vendorBootDTB(img []byte) ([]byte, error) {
	if len(img) < 2112 || string(img[:8]) != "VNDRBOOT" {
		return nil, fmt.Errorf("vendor_boot: bad magic")
	}
	le := binary.LittleEndian
	page := le.Uint32(img[12:])
	rdTotal := le.Uint32(img[24:])
	hdrSize := le.Uint32(img[2096:])
	dtbSize := le.Uint32(img[2100:])
	if page == 0 {
		return nil, fmt.Errorf("vendor_boot: zero page size")
	}
	if dtbSize == 0 {
		return nil, nil
	}
	off := page * (pages(hdrSize, page) + pages(rdTotal, page))
	if int(off+dtbSize) > len(img) {
		return nil, fmt.Errorf("vendor_boot: dtb section exceeds image")
	}
	return img[off : off+dtbSize], nil
}

func cstr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
