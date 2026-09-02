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
    std::string outputDir = concatenatePath(BUGREPORT_PACKING_DIR, "mailbox");
    if (mkdir(outputDir.c_str(), 0777) == -1) {
        printf("Unable to create folder: %s\n", outputDir.c_str());
        return 0;
    }

    copyFile("/sys/kernel/tracing/instances/goog_cpm_mailbox/trace",
             concatenatePath(outputDir.c_str(), "goog_cpm_mailbox_trace").c_str());
    copyFile("/sys/kernel/tracing/instances/goog_nq_mailbox/trace",
             concatenatePath(outputDir.c_str(), "goog_nq_mailbox_trace").c_str());

    return 0;
}
