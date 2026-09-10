// Copyright 2026 Terrance Leverette (AbstractsRevenge)
// Sovereign Lane Surgeon: https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon
// Licensed under the Apache License, Version 2.0.

package main

import "testing"

func set(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

func TestClassifyDep(t *testing.T) {
	cases := []struct {
		name     string
		lane     map[string]bool
		oracle   map[string]bool
		stock    map[string]bool
		wantDisp string
	}{
		// LANE-BUG: lane's own module, lowercase-m casing.
		{"Nexusmframework-core-sources", set(), set(), set(), "LANE-BUG"},
		{"NexusMframework-core-sources", set("NexusMframework-core-sources"), set(), set(), "LOAD"}, // correct casing (NexusM) + lane owns it -> LOAD, not LANE-BUG
		// DEAD: retired product-lane scaffolding.
		{"product_fork_src_systemuiclocks_bignum_apk", set(), set(), set(), "DEAD"},
		{"productowner_settingsgoogle_apk", set(), set(), set(), "DEAD"},
		// REPOINT: lane forks under a renamed form (prefix, lib-prefix, suffix).
		{"Calculator", set("NexusMCalculator"), set(), set("Calculator"), "REPOINT"},
		{"android-common", set("nexusm-android-common"), set(), set("android-common"), "REPOINT"},
		{"framework-res", set("framework-res-nexusm"), set("framework-res"), set("framework-res"), "REPOINT"},
		// LOAD: lane defines keep-name but the bp isn't loaded (else it wouldn't be undefined).
		{"framework-minus-apex-aconfig-java-defaults", set("framework-minus-apex-aconfig-java-defaults"), set(), set(), "LOAD"},
		// FORK: green oracle forks it (keep-name or Holo-prefixed), lane missing.
		{"services-config-update", set(), set("services-config-update"), set("services-config-update"), "FORK"},
		{"WallpaperPicker", set(), set("HoloWallpaperPicker"), set("WallpaperPicker"), "FORK"},
		// STOCK-GAP: stock defines it, not resolving for the lane, neither lane nor oracle forks.
		{"services.core", set(), set(), set("services.core"), "STOCK-GAP"},
		// derived-name resolution: .impl strips to the framework-nfc base found in stock.
		{"framework-nfc.impl", set(), set(), set("framework-nfc"), "STOCK-GAP"},
		// MISSING: nowhere.
		{"Gallery3D", set(), set(), set(), "MISSING"},
	}
	for _, c := range cases {
		got, _ := classifyDep(c.name, c.lane, c.oracle, c.stock)
		if got != c.wantDisp {
			t.Errorf("classifyDep(%q) = %s, want %s", c.name, got, c.wantDisp)
		}
	}
}

func TestIsTestOnly(t *testing.T) {
	cases := []struct {
		bps  []string
		want bool
	}{
		{[]string{"frameworks-nexusm/base/tests/Input/Android.bp"}, true},
		{[]string{"frameworks-nexusm/base/services/tests/wmtests/Android.bp"}, true},
		{[]string{"frameworks-nexusm/base/core/java/Android.bp"}, false},
		{[]string{"frameworks-nexusm/base/tests/Input/Android.bp", "packages-nexusm/apps/NexusMNfc/Android.bp"}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := isTestOnly(c.bps); got != c.want {
			t.Errorf("isTestOnly(%v) = %v, want %v", c.bps, got, c.want)
		}
	}
}
