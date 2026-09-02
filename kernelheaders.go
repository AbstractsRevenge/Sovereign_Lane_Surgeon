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
	"embed"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

// kernelheaders.go — the vendor kernel UAPI headers a kernel prebuilt dir must carry.
//
// WHY (observed 2026-09-02, cheetah `m droid superimage`, Build Capture run 143842Z): the
// device makefiles set TARGET_BOARD_KERNEL_HEADERS ?= $(RELEASE_KERNEL_<DEVICE>_DIR)/kernel-headers,
// Soong turns that into the kernel_headers module every vendor cc module may include from, and
// hardware/google/graphics/common/libhwc2.1 includes <drm/samsung_drm.h> from it (13 sources).
// config.mk's $(wildcard) makes a missing dir silently empty — `m nothing` passes, the compile does
// not. A CP2A factory image carries no headers at all, so they come from the last AOSP tag that
// shipped the family's kernel dir (assets/kernel_headers/MANIFEST records which): 9–10 files per
// family, byte-identical across the families of one SoC in android-15.0.0_r36.

//go:embed all:assets/kernel_headers
var embeddedKernelHeaders embed.FS

const kernelHeadersRoot = "assets/kernel_headers"

// kernelHeadersProvenance returns the manifest line's tag and build dir for family, or "".
func kernelHeadersProvenance(family string) (tag, build string) {
	b, err := embeddedKernelHeaders.ReadFile(path.Join(kernelHeadersRoot, "MANIFEST"))
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && !strings.HasPrefix(f[0], "#") && f[0] == family {
			return f[1], f[2]
		}
	}
	return "", ""
}

// putKernelHeaders writes the family's bundled kernel-headers/ into the assembled dir (no-clobber,
// through k.put) and records the provenance. Returns the number of files offered.
func putKernelHeaders(k *kernelAssembly, outRoot, family string) int {
	tag, build := kernelHeadersProvenance(family)
	if tag == "" {
		k.Notes = append(k.Notes, "kernel-headers: none bundled for family "+family+" — TARGET_BOARD_KERNEL_HEADERS will be empty and vendor code including <drm/samsung_drm.h> will not compile")
		return 0
	}
	src := path.Join(kernelHeadersRoot, family, "kernel-headers")
	n := 0
	_ = fs.WalkDir(embeddedKernelHeaders, src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, rerr := embeddedKernelHeaders.ReadFile(p)
		if rerr != nil {
			return nil
		}
		rel, _ := filepath.Rel(filepath.FromSlash(src), filepath.FromSlash(p))
		k.put(outRoot, filepath.Join("kernel-headers", rel), b)
		n++
		return nil
	})
	k.Notes = append(k.Notes, "kernel-headers: "+itoa(n)+" file(s) from "+build+" ("+tag+") — the factory image ships no headers")
	return n
}
