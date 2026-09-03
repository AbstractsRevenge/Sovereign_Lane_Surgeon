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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// vendorutils.go — the ONE vendor-image extraction path, shared by fetch-factory-image,
// create -stock -factory-images-root and the extract-vendor subcommand (it replaced two
// near-identical copies that had drifted: one skipped the chown on the rsync fallback).
//
// Privilege model (measured 2026-09-02 on CP2A factory images): every partition image this needs
// (vendor.img, vendor_dlkm.img, system_dlkm.img, system_ext.img) is a raw ext2/4 filesystem after simg2img, and
// e2fsprogs' `debugfs -R "rdump / <dst>"` reads it WITHOUT root. So the no-root path is tried
// first and the historical `sudo mount -o loop` path is only the fallback — which is what lets
// the kernel-dir assembly (kernelprebuilt.go) run in an unattended session with no sudo ticket.

// copyFilesWithCp copies files from src to dst using sudo cp -r.
// Used as a fallback when rsync is not available.
func copyFilesWithCp(src, dst string) error {
	// Create the destination directory
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	// Use cp -r to copy everything
	cmd := exec.Command("sudo", "cp", "-r", src+"/.", dst)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cp failed: %w", err)
	}
	return nil
}

// isSparseImage checks if a file is an Android sparse image.
func isSparseImage(path string) (bool, error) {
	fileCmd := exec.Command("file", path)
	output, err := fileCmd.Output()
	if err != nil {
		return false, fmt.Errorf("file command failed: %w", err)
	}
	return strings.Contains(string(output), "Android sparse"), nil
}

// convertSparseToRaw converts an Android sparse image to raw format using simg2img.
func convertSparseToRaw(sparsePath, rawPath string) error {
	cmd := exec.Command("simg2img", sparsePath, rawPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("simg2img failed: %w", err)
	}
	return nil
}

// mountImage mounts a raw image file to a mount point.
func mountImage(rawPath, mountPoint string) error {
	// Try with default options first
	mountCmd := exec.Command("sudo", "mount", "-o", "loop", rawPath, mountPoint)
	if err := mountCmd.Run(); err != nil {
		// Try with read-only if the first attempt fails
		mountCmd = exec.Command("sudo", "mount", "-o", "loop,ro", rawPath, mountPoint)
		if err := mountCmd.Run(); err != nil {
			return fmt.Errorf("mount failed: %w", err)
		}
	}
	return nil
}

// unmountImage unmounts a mount point and cleans up.
func unmountImage(mountPoint string) {
	exec.Command("sudo", "umount", mountPoint).Run()
}

// vendorImages are the partition images extracted and where their contents land under
// vendor/google_devices/<device>/. system_dlkm feeds the kernel-dir assembly's third module list.
var vendorImages = []struct{ img, subdir string }{
	{"vendor.img", "proprietary"},
	{"vendor_dlkm.img", "dlkm"},
	{"system_dlkm.img", "system_dlkm"},
	// system_ext.img holds the blobs the self-extractor lists under system_ext/ (IMS/RCS/UWB
	// apks, their permission xmls, libmediaadaptor); wireVendorBlobs reads them from here.
	{"system_ext.img", "system_ext"},
}

// extractVendorImages extracts every vendorImages entry found in factoryDeviceDir (the per-device
// factory extraction dir holding the flat partition images) into outRoot/vendor/google_devices/
// <device>/<subdir>/. Returns how many images were extracted; a missing image is skipped with a
// note, a failed one is an error.
func extractVendorImages(device, factoryDeviceDir, outRoot string) (int, error) {
	n := 0
	for _, im := range vendorImages {
		imgPath := filepath.Join(factoryDeviceDir, im.img)
		if _, err := os.Stat(imgPath); err != nil {
			fmt.Printf("  ~ %s not found (skipping)\n", im.img)
			continue
		}
		target := filepath.Join(outRoot, "vendor", "google_devices", device, im.subdir)
		fmt.Printf("  + extracting %s → vendor/google_devices/%s/%s/ ...\n", im.img, device, im.subdir)
		if err := extractImageTo(imgPath, target); err != nil {
			return n, fmt.Errorf("%s: %w", im.img, err)
		}
		n++
	}
	return n, nil
}

// extractImageTo unpacks one ext filesystem image (sparse or raw) into dst: simg2img if sparse,
// then debugfs rdump (no root), falling back to a loop mount + copy + chown when debugfs is
// unavailable or fails.
func extractImageTo(imgPath, dst string) error {
	sparse, err := isSparseImage(imgPath)
	if err != nil {
		return err
	}
	raw := imgPath
	if sparse {
		raw = imgPath + ".raw"
		if err := convertSparseToRaw(imgPath, raw); err != nil {
			return err
		}
		defer os.Remove(raw)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	if _, err := exec.LookPath("debugfs"); err == nil {
		if err := debugfsRdump(raw, dst); err == nil {
			return nil
		} else {
			fmt.Printf("    ~ debugfs extraction failed (%v); falling back to loop mount\n", err)
		}
	}
	mountPoint := imgPath + "_mount"
	if err := os.MkdirAll(mountPoint, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(mountPoint)
	if err := mountImage(raw, mountPoint); err != nil {
		return err
	}
	defer unmountImage(mountPoint)
	if rsync, _ := exec.LookPath("rsync"); rsync != "" {
		if err := exec.Command("sudo", "rsync", "-a", "--chown="+os.Getenv("USER"), mountPoint+"/.", dst).Run(); err == nil {
			return fixOwnership(dst)
		}
	}
	if err := copyFilesWithCp(mountPoint, dst); err != nil {
		return err
	}
	return fixOwnership(dst)
}

// debugfsRdump extracts the whole tree of a raw ext image into dst with e2fsprogs' debugfs, which
// needs no privileges. debugfs prints "Operation not permitted while changing ownership" per file
// when run unprivileged — the files are written regardless — so its stderr is not treated as
// failure; an empty result is.
func debugfsRdump(raw, dst string) error {
	cmd := exec.Command("debugfs", "-R", "rdump / "+dst, raw)
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("debugfs: %w", err)
	}
	ents, err := os.ReadDir(dst)
	if err != nil || len(ents) == 0 {
		return fmt.Errorf("debugfs produced no files under %s", dst)
	}
	return nil
}

// fixOwnership recursively changes ownership of a directory to the current user.
func fixOwnership(targetDir string) error {
	cmd := exec.Command("sudo", "chown", "-R", os.Getenv("USER"), targetDir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("chown failed: %w", err)
	}
	return nil
}
