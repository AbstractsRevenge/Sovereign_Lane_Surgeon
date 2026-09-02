/*
 * Copyright 2022 The Android Open Source Project
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
#include <android-base/file.h>
#include <android-base/properties.h>
#include <dirent.h>
#include <dump/pixel_dump.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#define GPS_LOG_NUMBER_PROPERTY "persist.vendor.gps.aol.log_num"
#define GPS_LOG_DIRECTORY "/data/vendor/gps/logs"
#define GPS_RESOURCE_DIRECTORY "/data/vendor/gps/resource"
#define GPS_TMP_LOG_DIRECTORY "/data/vendor/gps/logs/.tmp"
#define GPS_LOG_PREFIX "gl-"
#define GPS_MCU_LOG_PREFIX "esw-"
#define GPS_MALLOC_LOG_DIRECTORY "/data/vendor/gps"
#define GPS_MALLOC_LOG_PREFIX "malloc_"
#define GPS_VENDOR_CHIP_INFO "/data/vendor/gps/chip.info"
#define GPS_RAWLOG_PREFIX "rawbin"
#define GPS_MEMDUMP_LOG_PREFIX "memdump_"

static void copyDirectory(const std::string &source,
                          const std::string &outputDir) {
  DIR *dir = opendir(source.c_str());
  if (dir == nullptr) {
    return;
  }

  if (mkdir(outputDir.c_str(), 0777) == -1) {
    closedir(dir);
    return;
  }

  struct dirent *entry;
  while ((entry = readdir(dir)) != nullptr) {
    std::string entryName = entry->d_name;
    if (entryName == "." || entryName == "..") {
      continue;
    }

    std::string sourcePath = source + "/" + entryName;
    std::string destPath = outputDir + "/" + entryName;

    struct stat st;
    if (stat(sourcePath.c_str(), &st) == 0) {
      if (S_ISDIR(st.st_mode))
        copyDirectory(sourcePath, destPath);
      else
        copyFile(sourcePath.c_str(), destPath.c_str());
    }
  }
  closedir(dir);
  return;
}

int compareFileExtensions(const struct dirent **a, const struct dirent **b) {
    int num_a, num_b;
    sscanf((*a)->d_name, "rawbinlog.out.%d", &num_a);
    sscanf((*b)->d_name, "rawbinlog.out.%d", &num_b);

    return num_a - num_b;
}

void dumpLogsAscending(const char* SrcDir, const char* DestDir, int limit, const char* prefix) {

    struct dirent **dirent_list = NULL;
    int num_entries = scandir(SrcDir, &dirent_list, 0, (int (*)(const struct dirent **, const struct dirent **)) alphasort);
    if (!dirent_list) {
        printf("Unable to scan dir: %s.\n", SrcDir);
        return;
    } else if (num_entries <= 0) {
        printf("No file is found.\n");
        return;
    }

    if (access(DestDir, R_OK)) {
        printf("Unable to find folder: %s\n", DestDir);
        return;
    }

    qsort(dirent_list, num_entries, sizeof(struct dirent *), (int (*)(const void *, const void *)) compareFileExtensions);

    int copiedFiles = 0;

    for (int i = 0 ; i < num_entries; i++) {

        if (0 != strncmp(dirent_list[i]->d_name, prefix, strlen(prefix))) {
            continue;
        }

        if ((copiedFiles >= limit) && (limit != -1)) {
            printf("Skipped %s\n", dirent_list[i]->d_name);
            continue;
        }

        copiedFiles++;
        copyFile(concatenatePath(SrcDir, dirent_list[i]->d_name).c_str(), concatenatePath(DestDir, dirent_list[i]->d_name).c_str());
    }

    while (num_entries--) {
        free(dirent_list[num_entries]);
    }

    free(dirent_list);
    return;
}

int main() {
    if(!::android::base::GetBoolProperty("vendor.gps.aol.enabled", false)) {
        printf("vendor.gps.aol.enabled is false. gps logging is not running.\n");
        return 0;
    }
    int maxFileNum = ::android::base::GetIntProperty(GPS_LOG_NUMBER_PROPERTY, 20);
    std::string outputDir = concatenatePath(BUGREPORT_PACKING_DIR, "gps");
    if (mkdir(outputDir.c_str(), 0777) == -1) {
        printf("Unable to create folder: %s\n", outputDir.c_str());
        return 0;
    }

    dumpLogs(GPS_TMP_LOG_DIRECTORY, outputDir.c_str(), 1, GPS_LOG_PREFIX);
    dumpLogs(GPS_LOG_DIRECTORY, outputDir.c_str(), 3, GPS_MCU_LOG_PREFIX);
    dumpLogs(GPS_LOG_DIRECTORY, outputDir.c_str(), maxFileNum, GPS_LOG_PREFIX);
    dumpLogs(GPS_MALLOC_LOG_DIRECTORY, outputDir.c_str(), 1, GPS_MALLOC_LOG_PREFIX);
    if (access(GPS_VENDOR_CHIP_INFO, F_OK) == 0) {
        copyFile(GPS_VENDOR_CHIP_INFO, concatenatePath(outputDir.c_str(), "chip.info").c_str());
    }
    dumpLogsAscending(GPS_LOG_DIRECTORY, outputDir.c_str(), 5, GPS_RAWLOG_PREFIX);
    dumpLogs(GPS_LOG_DIRECTORY, outputDir.c_str(), 18, GPS_MEMDUMP_LOG_PREFIX);
    copyDirectory(GPS_RESOURCE_DIRECTORY, concatenatePath(outputDir.c_str(), "resource"));
    return 0;
}
