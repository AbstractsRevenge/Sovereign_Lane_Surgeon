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
	"fmt"
	"strconv"
)

// cpio.go — a reader for the SVR4 "newc" cpio archives Android ramdisks are packed in (magic
// 070701, thirteen 8-hex-digit fields, name and data each padded to 4 bytes, "TRAILER!!!" ends
// the archive). Pure Go, stdlib only, like lz4legacy.go.

type cpioEntry struct {
	Name string
	Mode uint32
	Data []byte
}

func (e cpioEntry) isRegular() bool { return e.Mode&0170000 == 0100000 }

func cpioNewcEntries(a []byte) ([]cpioEntry, error) {
	var out []cpioEntry
	i := 0
	hex := func(off int) (uint32, error) {
		v, err := strconv.ParseUint(string(a[off:off+8]), 16, 32)
		return uint32(v), err
	}
	for {
		if i+110 > len(a) {
			return nil, fmt.Errorf("cpio: truncated header at %d", i)
		}
		if string(a[i:i+6]) != "070701" {
			return nil, fmt.Errorf("cpio: bad magic %q at %d (only newc is supported)", a[i:i+6], i)
		}
		mode, err := hex(i + 14)
		if err != nil {
			return nil, err
		}
		size, err := hex(i + 54)
		if err != nil {
			return nil, err
		}
		nameLen, err := hex(i + 94)
		if err != nil {
			return nil, err
		}
		nameStart := i + 110
		if nameStart+int(nameLen) > len(a) {
			return nil, fmt.Errorf("cpio: truncated name at %d", nameStart)
		}
		name := string(a[nameStart : nameStart+int(nameLen)-1]) // drop NUL
		dataStart := (nameStart + int(nameLen) + 3) &^ 3
		if name == "TRAILER!!!" {
			return out, nil
		}
		if dataStart+int(size) > len(a) {
			return nil, fmt.Errorf("cpio: truncated data for %q", name)
		}
		out = append(out, cpioEntry{Name: name, Mode: mode, Data: a[dataStart : dataStart+int(size)]})
		i = (dataStart + int(size) + 3) &^ 3
	}
}
