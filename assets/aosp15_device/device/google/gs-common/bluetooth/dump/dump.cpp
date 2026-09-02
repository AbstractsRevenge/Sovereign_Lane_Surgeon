/*
 * Copyright 2023 The Android Open Source Project
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

#define PIXELBT_ACTIVITY_LOG_DIRECTORY "/data/vendor/bluetooth"
#define PIXELBT_SNOOP_LOG_DIRECTORY "/data/vendor/bluetooth"
#define PIXELBT_FW_LOG_DIRECTORY "/data/vendor/ssrdump/coredump"
#define PIXELBT_SNOOP_LOG_PREFIX "btsnoop_hci_vnd"
#define PIXELBT_BACKUP_SNOOP_LOG_PREFIX "backup_btsnoop_hci_vnd"
#define PIXELBT_FW_DUMP_LOG_PREFIX "coredump_bt_socdump_"
#define PIXELBT_CHRE_DUMP_LOG_PREFIX "coredump_bt_chredump_"
#define PIXELBT_HAL_DUMP_LOG_PREFIX "coredump_bt_"
#define PIXELBT_ACTIVITY_LOG_PREFIX "bt_activity_"

int main() {
    std::string outputDir = concatenatePath(BUGREPORT_PACKING_DIR, "bt");
    if (mkdir(outputDir.c_str(), 0777) == -1) {
        printf("Unable to create folder: %s\n", outputDir.c_str());
        return 0;
    }

    dumpLogs(PIXELBT_SNOOP_LOG_DIRECTORY, outputDir.c_str(), 4,
             PIXELBT_SNOOP_LOG_PREFIX);
    dumpLogs(PIXELBT_SNOOP_LOG_DIRECTORY, outputDir.c_str(), 2, PIXELBT_BACKUP_SNOOP_LOG_PREFIX);
    dumpLogs(PIXELBT_FW_LOG_DIRECTORY, outputDir.c_str(), 10, PIXELBT_FW_DUMP_LOG_PREFIX);
    dumpLogs(PIXELBT_FW_LOG_DIRECTORY, outputDir.c_str(), 10, PIXELBT_CHRE_DUMP_LOG_PREFIX);
    dumpLogs(PIXELBT_FW_LOG_DIRECTORY, outputDir.c_str(), 10, PIXELBT_HAL_DUMP_LOG_PREFIX);
    dumpLogs(PIXELBT_ACTIVITY_LOG_DIRECTORY, outputDir.c_str(), 10, PIXELBT_ACTIVITY_LOG_PREFIX);
    return 0;
}
