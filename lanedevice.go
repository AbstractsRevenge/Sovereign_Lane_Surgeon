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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// requalifyLaneDeviceText moves the source lane's physical roots and product identities onto the
// new lane. It deliberately leaves the source lane's module and Soong-config names unchanged:
// a lane derived from Holo inherits its Holo UX modules, while only its lane/product path identity
// changes (aosp_cheetah_holo -> aosp_cheetah_holo2, frameworks-holo -> frameworks-holo2).
// Lane-owned soong_config namespaces and variables also move with the lane because the cloned
// Blueprint select() conditions are requalified the same way by runRequalifyFromLane.
func requalifyLaneDeviceText(b []byte, family, srcLane, newLane string) []byte {
	replacements := [][2]string{
		{"frameworks-" + srcLane, "frameworks-" + newLane},
		{"packages-" + srcLane, "packages-" + newLane},
		{"device/google/" + family + "-" + srcLane, "device/google/" + family + "-" + newLane},
		{"device_google_" + family + "_" + srcLane + "_license", "device_google_" + family + "_" + newLane + "_license"},
		{"SOONG_CONFIG_" + srcLane + "_", "SOONG_CONFIG_" + newLane + "_"},
		{srcLane + "_framework_routing", newLane + "_framework_routing"},
		{srcLane + "_package_routing", newLane + "_package_routing"},
		{"enable_" + srcLane + "_", "enable_" + newLane + "_"},
	}
	for _, replacement := range replacements {
		b = bytes.ReplaceAll(b, []byte(replacement[0]), []byte(replacement[1]))
	}
	for _, prefix := range []string{"aosp_", "device-"} {
		rx := regexp.MustCompile(`\b(` + regexp.QuoteMeta(prefix) + `[A-Za-z0-9]+_)` + regexp.QuoteMeta(srcLane) + `\b`)
		b = rx.ReplaceAll(b, []byte(`${1}`+newLane))
	}
	return b
}

func laneDeviceRelPath(rel, srcLane, newLane string) string {
	return strings.ReplaceAll(rel, "_"+srcLane, "_"+newLane)
}

func copyDeviceFamilyFromLane(c LaneConfig, outRoot, family string) (copied, skipped int, err error) {
	src := filepath.Join(outRoot, "device", "google", family+"-"+c.FromLane)
	dst := filepath.Join(outRoot, "device", "google", family+"-"+c.Name)
	err = filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if base := filepath.Base(path); base == ".git" || base == ".repo" {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		target := filepath.Join(dst, laneDeviceRelPath(rel, c.FromLane, c.Name))
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if _, statErr := os.Lstat(target); statErr == nil {
			skipped++
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			link = string(requalifyLaneDeviceText([]byte(link), family, c.FromLane, c.Name))
			if err := os.Symlink(link, target); err != nil {
				return err
			}
			copied++
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Lane/product tokens occur in text build and configuration files. Avoid interpreting
		// protobufs, images, firmware, or other copied payloads as text.
		if len(data) <= 8<<20 && !bytes.ContainsRune(data, '\x00') {
			data = requalifyLaneDeviceText(data, family, c.FromLane, c.Name)
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
			return err
		}
		copied++
		return nil
	})
	return copied, skipped, err
}

// writeDeviceProductsFromLane preserves a proven source lane's complete device/product wiring.
// The generic device templates are appropriate for a stock-seeded lane, but would discard a
// source lane's curated package list, overlays, signing identity, properties, and hardware paths.
func writeDeviceProductsFromLane(c LaneConfig, outRoot string) (wrote, skipped int, handled, fatal bool) {
	families := []string{}
	seen := map[string]bool{}
	for _, product := range c.Devices {
		res := resolveDevice(outRoot, product)
		if !seen[res.Family] {
			seen[res.Family] = true
			families = append(families, res.Family)
		}
	}
	for _, family := range families {
		src := filepath.Join(outRoot, "device", "google", family+"-"+c.FromLane)
		if info, err := os.Stat(src); err != nil || !info.IsDir() {
			return 0, 0, false, false
		}
	}
	for _, family := range families {
		copied, already, err := copyDeviceFamilyFromLane(c, outRoot, family)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! inherit device/google/%s-%s: %v\n", family, c.FromLane, err)
			return wrote, skipped, true, true
		}
		fmt.Printf("  device family %s: device/google/%s-%s → device/google/%s-%s (%d files, %d existing)\n",
			family, family, c.FromLane, family, c.Name, copied, already)
		wrote += copied
		skipped += already
	}
	return wrote, skipped, true, false
}
