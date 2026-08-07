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
	"strings"
	"testing"
)

func TestGenCuttlefishProduct(t *testing.T) {
	cfg := deriveLane("holo", true, nil, true, true, "")
	cases := map[string]struct {
		form     cfForm
		wantFile string
		wantName string
		wantChar bool // tablet sets PRODUCT_CHARACTERISTICS
	}{
		"phone":  {cfForms[0], "aosp_cf_holo.mk", "aosp_cf_x86_64_phone_holo", false},
		"tablet": {cfForms[1], "aosp_cf_tablet_holo.mk", "aosp_cf_x86_64_tablet_holo", true},
	}
	for name, c := range cases {
		rel, content, err := genCuttlefishProduct(cfg, c.form)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.HasSuffix(rel, "/vsoc_x86_64/phone/"+c.wantFile) {
			t.Errorf("%s: path = %q, want suffix /vsoc_x86_64/phone/%s", name, rel, c.wantFile)
		}
		if !strings.Contains(content, "PRODUCT_NAME := "+c.wantName) {
			t.Errorf("%s: missing PRODUCT_NAME := %s", name, c.wantName)
		}
		if got := strings.Contains(content, "PRODUCT_CHARACTERISTICS := tablet"); got != c.wantChar {
			t.Errorf("%s: PRODUCT_CHARACTERISTICS=tablet present=%v, want %v", name, got, c.wantChar)
		}
		// lane-parameterized inherit + stock base
		if !strings.Contains(content, "shared/holo/common.mk") {
			t.Errorf("%s: missing lane common inherit shared/holo/common.mk", name)
		}
		if !strings.Contains(content, "vsoc_x86_64/phone/aosp_cf.mk") {
			t.Errorf("%s: missing stock cuttlefish base inherit", name)
		}
	}
}
