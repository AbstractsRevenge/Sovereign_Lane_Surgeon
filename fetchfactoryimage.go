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
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// fetchfactoryimage.go — downloads a Google Pixel factory image for a device this project targets
// and extracts the partition images device revival needs (kernel + vendor blobs), from a small,
// hand-verified manifest of (device, build, URL, SHA-256) entries — NOT a live scrape of
// developers.google.com/android/images. That page's download table is populated by client-side JS
// after an explicit "Acknowledge terms" click and is NOT present in the page's raw HTML (confirmed
// directly: a plain HTTP GET of the page returns a ~75KB shell with zero dl.google.com references).
// Rendering it would need a headless browser — a dependency this self-contained tool deliberately
// doesn't take on. The actual dl.google.com download URLs themselves ARE plain, unauthenticated
// static files once known (confirmed: HTTP 200, Accept-Ranges, no cookie/session required) — so a
// small manifest of confirmed URLs + their published SHA-256 checksums, refreshed by hand when
// needed, gets the automation win with zero new dependencies and zero scraping.
//
// LEGAL: this command downloads Google's copyrighted, licensed factory images. It embeds Google's
// own real terms and conditions text from that page verbatim (see googleFactoryImageTerms) and
// REFUSES to download anything until the user explicitly accepts them for this run (interactively,
// or via -i-accept-google-terms for scripted use) — the same consent gate Google's own page
// enforces, reproduced faithfully rather than bypassed. Google's terms explicitly prohibit
// redistribution; this command only ever downloads FROM Google TO the user's own machine, exactly
// what they'd get clicking the link in a browser themselves — it does not host, cache, or
// redistribute the images anywhere else.
//
// Output layout matches a manually-extracted factory image exactly (flat partition images +
// nested <device>-<build>/ dir with bootloader/radio) — <out>/<device>/ from this command is
// drop-in compatible with `create -stock -factory-images-root <out>` (vendorblobs.go).
//
// v0.4.0 enhancement: after extracting partition images, also extracts the contents of vendor.img
// and vendor_dlkm.img (sparse → raw → mount → copy) so the vendor/google_devices/<device>/
// directory is fully populated with the actual blob files, not just the partition image files.

const googleFactoryImageTerms = `Google factory-image Terms and Conditions
(verbatim from https://developers.google.com/android/images — read before accepting)

This page contains binary image files that allow you to restore your Nexus or Pixel
device's original factory firmware. You will find these files useful if you have
flashed custom builds on your device, and want to return your device to its factory
state.

Note that it's typically easier and safer to sideload the full OTA image instead
(https://developers.google.com/android/ota).

If you do use a factory image, make sure that you relock your bootloader when the
process is complete.

These files are for use only on your personal Nexus or Pixel devices and may not be
disassembled, decompiled, reverse engineered, modified or redistributed by you or
used in any way except as specifically set forth in the license terms that came
with your device.

Warning: Installing a factory image will erase all data from the device, and
unlocking the bootloader will make your device less secure. In most cases it should
be possible to sideload the full OTA image instead. This does not require a data
wipe, and does not require the bootloader to be unlocked.

While it may be possible to restore certain data backed up to your Google Account,
apps and their associated data will be uninstalled. Before proceeding, ensure that
data you would like to retain is backed up to your Google Account
(https://support.google.com/nexus/answer/2819582#backup).

Downloading of the system image and use of the device software is subject to the
Google Terms of Service (https://www.google.com/intl/en/policies/terms/). By
continuing, you agree to the Google Terms of Service and Privacy Policy
(https://www.google.com/intl/en/policies/privacy/). Your downloading of the system
image and use of the device software may also be subject to certain third-party
terms of service, which can be found in Settings > About phone > Legal information,
or as otherwise provided.

Source: https://developers.google.com/android/images
`

type factoryImageEntry struct {
	Device string
	Build  string // e.g. "CP2A.260705.006"
	URL    string
	SHA256 string
}

// factoryImageManifest is a hand-verified snapshot of developers.google.com/android/images (the
// page's table read after its Acknowledge gate) for every device android-17's cp2a release names
// AND an AOSP device tree exists for: the Pixel 7 family on 2026-09-01 (cp2a.260705.006), the
// other twelve on 2026-09-02 — each device's LATEST CP2A build at verification time (gs101/gs201
// devices on the July build, zuma/zumapro devices on the August one). The Pixel 10 family (blazer,
// frankel, mustang, rango, stallion) has CP2A images but no AOSP tree in any tag, so it is not
// listed. It is NOT auto-refreshed: Google publishes new builds regularly (monthly
// security updates), and discovering them requires a human to visit the page, click Acknowledge,
// and read the table — see the file-level doc comment for why that can't be automated safely or
// cheaply. To refresh or add an entry: visit the Source URL below, Acknowledge, find your device's
// table, copy its Download link + SHA-256 Checksum cell verbatim, add a line here.
var factoryImageManifest = []factoryImageEntry{
	{Device: "panther", Build: "CP2A.260705.006", URL: "https://dl.google.com/dl/android/aosp/panther-cp2a.260705.006-factory-ed94a24e.zip", SHA256: "ed94a24e693a28f236e87c9e03436871c2dd6b03b4b56c5839598115d0372b0b"},
	{Device: "cheetah", Build: "CP2A.260705.006", URL: "https://dl.google.com/dl/android/aosp/cheetah-cp2a.260705.006-factory-23d564ad.zip", SHA256: "23d564ad5d0db2d04fa4bbde43930c9b0f8e570a51e8090c8d37c71dc96a2c0c"},
	{Device: "lynx", Build: "CP2A.260705.006", URL: "https://dl.google.com/dl/android/aosp/lynx-cp2a.260705.006-factory-ba6e0e87.zip", SHA256: "ba6e0e873d9350c1e246050b892e17ed7650cef1ded20411b25df74a763bd6eb"},
	{Device: "tangorpro", Build: "CP2A.260705.006", URL: "https://dl.google.com/dl/android/aosp/tangorpro-cp2a.260705.006-factory-e3afed5f.zip", SHA256: "e3afed5fa2d15d0aa11f9913ce6f03142948386512c55c69b5c46931246c6b46"},
	{Device: "oriole", Build: "CP2A.260705.006.A1", URL: "https://dl.google.com/dl/android/aosp/oriole-cp2a.260705.006.a1-factory-218b72cb.zip", SHA256: "218b72cb8267cd61bdd24b42e951d90716ca84bb37de56e85f5f08b5d3c20d61"},
	{Device: "raven", Build: "CP2A.260705.006.A1", URL: "https://dl.google.com/dl/android/aosp/raven-cp2a.260705.006.a1-factory-133b7b44.zip", SHA256: "133b7b441b72df3d7bd819d4dbc033f94fc69f29a5dc7f06e849503f4537f934"},
	{Device: "bluejay", Build: "CP2A.260705.006.A1", URL: "https://dl.google.com/dl/android/aosp/bluejay-cp2a.260705.006.a1-factory-d1871b52.zip", SHA256: "d1871b52c28486dfb5410ad24892043f571a7a683cb89c430c1fea2b4ed5ad9e"},
	{Device: "felix", Build: "CP2A.260705.006", URL: "https://dl.google.com/dl/android/aosp/felix-cp2a.260705.006-factory-d6838fb2.zip", SHA256: "d6838fb2ccad5f32bd79ddb0bb60b7a6a3966c264ff9e56fea462ee099533e70"},
	{Device: "shiba", Build: "CP2A.260805.005", URL: "https://dl.google.com/dl/android/aosp/shiba-cp2a.260805.005-factory-26ca3017.zip", SHA256: "26ca3017652d5df8d5002a449ef079637061165de02396634514387602aad177"},
	{Device: "husky", Build: "CP2A.260805.005", URL: "https://dl.google.com/dl/android/aosp/husky-cp2a.260805.005-factory-5ca61b46.zip", SHA256: "5ca61b46ecc1b72a1d38d96bb2fc8d228535417cfa652c631cdbc996f97e2aa3"},
	{Device: "akita", Build: "CP2A.260805.005", URL: "https://dl.google.com/dl/android/aosp/akita-cp2a.260805.005-factory-b143bf41.zip", SHA256: "b143bf4128968df345dfb8ae51d09598084249606ae222a7bfd7c2c54f8c1a0c"},
	{Device: "tokay", Build: "CP2A.260805.005.A1", URL: "https://dl.google.com/dl/android/aosp/tokay-cp2a.260805.005.a1-factory-32d17c98.zip", SHA256: "32d17c98e1951ddcd1dba68f28b63466aa5339b4428cb1f1b2c58590f99180c6"},
	{Device: "caiman", Build: "CP2A.260805.005.A1", URL: "https://dl.google.com/dl/android/aosp/caiman-cp2a.260805.005.a1-factory-beae4cb2.zip", SHA256: "beae4cb2639a8a0c467a4444b784af0217b90cfcb111c25a23b42c18db3898c9"},
	{Device: "komodo", Build: "CP2A.260805.005.A1", URL: "https://dl.google.com/dl/android/aosp/komodo-cp2a.260805.005.a1-factory-40742477.zip", SHA256: "40742477715f15f4c10aae42022b4fbff877c9b5dc85902b1c9217576bad41d4"},
	{Device: "comet", Build: "CP2A.260805.005.A1", URL: "https://dl.google.com/dl/android/aosp/comet-cp2a.260805.005.a1-factory-450cf07a.zip", SHA256: "450cf07ae02151a9a9cc30aebe32757e3a48fe9a67d18aa6df291d28dd83fba7"},
	{Device: "tegu", Build: "CP2A.260805.005", URL: "https://dl.google.com/dl/android/aosp/tegu-cp2a.260805.005-factory-a3582e12.zip", SHA256: "a3582e120cf59e54ffbbe7a9d430987dd4d9ffb3ce691f9003724d80d4545026"},
}

func lookupFactoryImage(device string) (factoryImageEntry, bool) {
	for _, e := range factoryImageManifest {
		if e.Device == device {
			return e, true
		}
	}
	return factoryImageEntry{}, false
}

func knownFactoryImageDevices() string {
	names := make([]string, 0, len(factoryImageManifest))
	for _, e := range factoryImageManifest {
		names = append(names, e.Device)
	}
	return strings.Join(names, ", ")
}

// ─── End v0.4.0 enhancement ───

// cmdFetchFactoryImage downloads+extracts one manifest entry. Refuses to proceed until Google's
// terms are accepted for this run (confirmGoogleTerms).
//
// v0.4.0 enhancement: after extracting partition images, also extracts the contents of vendor.img
// and vendor_dlkm.img into vendor/google_devices/<device>/.
func cmdFetchFactoryImage(args []string) int {
	fs := flag.NewFlagSet("fetch-factory-image", flag.ExitOnError)
	device := fs.String("device", "", "device codename (known: "+knownFactoryImageDevices()+")")
	out := fs.String("out", "", "directory to extract into — writes <out>/<device>/, drop-in compatible with `create -stock -factory-images-root <out>`")
	cacheDir := fs.String("cache-dir", "", "where to download the factory zip (default: <out>/.factory-image-cache)")
	acceptTerms := fs.Bool("i-accept-google-terms", false, "accept Google's factory-image terms non-interactively (for scripted use — read them first: run without this flag once)")
	keepZip := fs.Bool("keep-zip", false, "keep the downloaded/intermediate zips after extraction (default: delete — saves ~4GB/device)")
	extractVendor := fs.Bool("extract-vendor", true, "extract vendor.img and vendor_dlkm.img contents into vendor/google_devices/<device>/ (default: true)")
	_ = fs.Parse(args)

	if *device == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "fetch-factory-image: -device <name> and -out <dir> are required")
		return 2
	}
	entry, ok := lookupFactoryImage(*device)
	if !ok {
		fmt.Fprintf(os.Stderr, "fetch-factory-image: %q is not in the manifest (known: %s)\n", *device, knownFactoryImageDevices())
		return 2
	}
	if !confirmGoogleTerms(*acceptTerms) {
		fmt.Fprintln(os.Stderr, "fetch-factory-image: terms not accepted — aborting, nothing downloaded")
		return 1
	}

	if *cacheDir == "" {
		*cacheDir = filepath.Join(*out, ".factory-image-cache")
	}
	if err := os.MkdirAll(*cacheDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "fetch-factory-image:", err)
		return 1
	}
	zipPath := filepath.Join(*cacheDir, filepath.Base(entry.URL))

	fmt.Printf("Fetching %s %s from %s\n", entry.Device, entry.Build, entry.URL)
	if err := downloadWithResume(entry.URL, zipPath); err != nil {
		fmt.Fprintln(os.Stderr, "fetch-factory-image: download:", err)
		return 1
	}
	fmt.Println("Verifying SHA-256...")
	if err := verifySHA256(zipPath, entry.SHA256); err != nil {
		fmt.Fprintln(os.Stderr, "fetch-factory-image:", err)
		return 1
	}
	fmt.Println("Checksum OK.")

	deviceOut := filepath.Join(*out, entry.Device)
	if err := os.MkdirAll(deviceOut, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "fetch-factory-image:", err)
		return 1
	}
	fmt.Println("Extracting outer archive...")
	if err := extractZip(zipPath, deviceOut); err != nil {
		fmt.Fprintln(os.Stderr, "fetch-factory-image: extract outer zip:", err)
		return 1
	}
	innerZip, ferr := findInnerImageZip(deviceOut)
	if ferr != nil {
		fmt.Fprintln(os.Stderr, "fetch-factory-image:", ferr)
		return 1
	}
	fmt.Printf("Extracting partition images from %s...\n", filepath.Base(innerZip))
	if err := extractZip(innerZip, deviceOut); err != nil {
		fmt.Fprintln(os.Stderr, "fetch-factory-image: extract inner zip:", err)
		return 1
	}
	if !*keepZip {
		os.Remove(innerZip)
		os.Remove(zipPath)
		fmt.Println("Removed intermediate zip(s) (pass -keep-zip to retain them).")
	}

	// v0.4.0 enhancement: Extract vendor image contents
	if *extractVendor {
		fmt.Println("Extracting vendor image contents...")
		// deviceOut is the factory directory containing vendor.img, vendor_dlkm.img, etc.
		// out is the root directory where we want vendor/google_devices/<device>/ created
		if _, err := extractVendorImages(entry.Device, deviceOut, *out); err != nil {
			fmt.Fprintf(os.Stderr, "fetch-factory-image: vendor image extraction: %v\n", err)
			// Don't fail - vendor.img might already be extracted or not needed
			fmt.Println("  ~ vendor image extraction had issues; you may need to extract manually")
		}
	}

	fmt.Printf("Done: %s\n", deviceOut)
	return 0
}

// confirmGoogleTerms prints Google's real terms verbatim and requires the user to type "I agree"
// before returning true — the tool's own reproduction of the consent gate Google's page enforces,
// not a bypass of it. -i-accept-google-terms is the scripted-use escape hatch (mirrors how
// sdkmanager --licenses / terraform / docker handle EULA acceptance), but the terms are always
// printed on stdout regardless so an operator running this via CI still has the text on record.
func confirmGoogleTerms(autoAccept bool) bool {
	fmt.Print(googleFactoryImageTerms)
	if autoAccept {
		fmt.Println("\n(accepted via -i-accept-google-terms)")
		return true
	}
	fmt.Print("\nType \"I agree\" to accept these terms and start the download: ")
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line) == "I agree"
}

// downloadWithResume downloads url to dst, resuming from dst's current size if it already exists
// and the server supports range requests (confirmed: Google's download host sends Accept-Ranges).
func downloadWithResume(url, dst string) error {
	var startAt int64
	if fi, err := os.Stat(dst); err == nil {
		startAt = fi.Size()
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if startAt > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(startAt, 10)+"-")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var f *os.File
	switch resp.StatusCode {
	case http.StatusOK:
		startAt = 0 // server ignored/doesn't support Range — start over
		f, err = os.Create(dst)
	case http.StatusPartialContent:
		f, err = os.OpenFile(dst, os.O_WRONLY|os.O_APPEND, 0o644)
	default:
		return fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}
	if err != nil {
		return err
	}
	defer f.Close()

	pw := &progressWriter{written: startAt, total: startAt + resp.ContentLength, label: filepath.Base(dst)}
	_, err = io.Copy(io.MultiWriter(f, pw), resp.Body)
	pw.finish()
	return err
}

type progressWriter struct {
	written, total int64
	label          string
	lastPrint      time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.written += int64(len(b))
	if time.Since(p.lastPrint) > 2*time.Second {
		p.lastPrint = time.Now()
		fmt.Printf("\r  %s: %.0f%% (%d/%d MB)  ", p.label, 100*float64(p.written)/float64(p.total), p.written/1e6, p.total/1e6)
	}
	return len(b), nil
}

func (p *progressWriter) finish() {
	fmt.Printf("\r  %s: 100%% (%d MB)          \n", p.label, p.written/1e6)
}

func verifySHA256(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch: got %s, want %s (the download may be corrupt/incomplete — delete %s and retry)", got, want, path)
	}
	return nil
}

// extractZip extracts src into destDir, rejecting any entry whose path would escape destDir
// (zip-slip) before writing anything for that entry.
func extractZip(src, destDir string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	cleanDest := filepath.Clean(destDir)
	for _, f := range r.File {
		target := filepath.Join(cleanDest, f.Name)
		if rel, rerr := filepath.Rel(cleanDest, target); rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("zip entry %q escapes destination directory", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractZipFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode()|0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

// findInnerImageZip locates the "image-<device>-<build>.zip" the outer factory zip's
// <device>-<build>/ subdirectory carries — the actual partition images (boot.img, vendor.img,
// vendor_dlkm.img, etc.) live inside IT, not the outer zip.
func findInnerImageZip(dir string) (string, error) {
	var found string
	err := filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || found != "" {
			return nil
		}
		if strings.HasPrefix(filepath.Base(p), "image-") && strings.HasSuffix(p, ".zip") {
			found = p
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("no image-*.zip found under %s (unexpected factory zip layout)", dir)
	}
	return found, nil
}
