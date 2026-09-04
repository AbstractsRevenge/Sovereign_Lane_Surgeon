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
	"fmt"
)

// lz4legacy.go — a pure-Go decoder for the LZ4 "legacy" frame (magic 0x184C2102) that Android
// vendor ramdisks and kernel payloads are compressed with. Kept in-tree so the surgeon stays a
// single `go build` with zero external deps: this host has no lz4 binary and no python lz4
// module, and the alternative (shelling out) would make kernel assembly depend on the operator's
// package set. The legacy frame is a sequence of blocks, each `uint32 LE compressed size` followed
// by one LZ4 block; blocks decompress to at most 8 MiB; the stream ends at EOF or at another magic.

const (
	lz4LegacyMagic     = 0x184C2102
	lz4LegacyBlockMax  = 8 << 20
	lz4MinMatch        = 4
	lz4MaxSaneOutBytes = 1 << 30 // refuse anything that would inflate past 1 GiB
)

// lz4BlockDecode appends the decompression of one LZ4 block to dst and returns the result.
func lz4BlockDecode(dst, src []byte) ([]byte, error) {
	base := len(dst)
	i := 0
	for i < len(src) {
		token := src[i]
		i++
		// literals
		lit := int(token >> 4)
		if lit == 15 {
			for {
				if i >= len(src) {
					return nil, fmt.Errorf("lz4: truncated literal length")
				}
				b := src[i]
				i++
				lit += int(b)
				if b != 255 {
					break
				}
			}
		}
		if i+lit > len(src) {
			return nil, fmt.Errorf("lz4: literal run past end of block")
		}
		dst = append(dst, src[i:i+lit]...)
		i += lit
		if i >= len(src) {
			break // last sequence carries literals only
		}
		if len(dst)-base > lz4MaxSaneOutBytes {
			return nil, fmt.Errorf("lz4: output exceeds sanity limit")
		}
		// match
		if i+2 > len(src) {
			return nil, fmt.Errorf("lz4: truncated match offset")
		}
		off := int(binary.LittleEndian.Uint16(src[i:]))
		i += 2
		if off == 0 || off > len(dst) {
			return nil, fmt.Errorf("lz4: bad match offset %d at out %d", off, len(dst))
		}
		ml := int(token & 0xF)
		if ml == 15 {
			for {
				if i >= len(src) {
					return nil, fmt.Errorf("lz4: truncated match length")
				}
				b := src[i]
				i++
				ml += int(b)
				if b != 255 {
					break
				}
			}
		}
		ml += lz4MinMatch
		start := len(dst) - off
		for k := 0; k < ml; k++ { // byte-wise: matches may overlap their own output
			dst = append(dst, dst[start+k])
		}
	}
	return dst, nil
}

// lz4LegacyDecode decompresses a whole legacy-frame stream (one or more concatenated frames).
func lz4LegacyDecode(src []byte) ([]byte, error) {
	if len(src) < 4 || binary.LittleEndian.Uint32(src) != lz4LegacyMagic {
		return nil, fmt.Errorf("lz4: not a legacy frame (magic %#x)", binary.LittleEndian.Uint32(src))
	}
	var out []byte
	i := 4
	for i+4 <= len(src) {
		n := binary.LittleEndian.Uint32(src[i:])
		if n == lz4LegacyMagic { // concatenated frame
			i += 4
			continue
		}
		i += 4
		if n == 0 || int(n) > len(src)-i {
			return nil, fmt.Errorf("lz4: block size %d exceeds remaining %d", n, len(src)-i)
		}
		before := len(out)
		var err error
		out, err = lz4BlockDecode(out, src[i:i+int(n)])
		if err != nil {
			return nil, err
		}
		if len(out)-before > lz4LegacyBlockMax {
			return nil, fmt.Errorf("lz4: block inflated past the 8 MiB legacy limit")
		}
		i += int(n)
	}
	return out, nil
}
