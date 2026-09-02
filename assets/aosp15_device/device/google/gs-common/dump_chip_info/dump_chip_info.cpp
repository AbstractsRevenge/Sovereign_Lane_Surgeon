/*
 * Copyright 2024 The Android Open Source Project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
#include <dump/pixel_dump.h>
#include <android-base/file.h>

int main() {
    dumpFileContent("ap_rom_patch_ver", "/sys/devices/system/goog-chip-info/ap_rom_patch_ver");
    dumpFileContent("gpcm_asic_id", "/sys/devices/system/goog-chip-info/gpcm_asic_id");
    dumpFileContent("device_table", "/sys/devices/system/goog-chip-info/device_table");
    dumpFileContent("dvfs_table", "/sys/devices/system/goog-chip-info/dvfs_table");
    dumpFileContent("hw_feature_table", "/sys/devices/system/goog-chip-info/hw_feature_table");
    dumpFileContent("ids_table", "/sys/devices/system/goog-chip-info/ids_table");

    return 0;
}
