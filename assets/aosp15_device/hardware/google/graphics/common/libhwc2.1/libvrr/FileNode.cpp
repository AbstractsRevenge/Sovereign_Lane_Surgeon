/*
 * Copyright (C) 2024 The Android Open Source Project
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

#define ATRACE_TAG (ATRACE_TAG_GRAPHICS | ATRACE_TAG_HAL)
#include "FileNode.h"
#include <log/log.h>
#include <utils/Trace.h>
#include <sstream>

namespace android {

ANDROID_SINGLETON_STATIC_INSTANCE(hardware::graphics::composer::FileNodeManager);

namespace hardware::graphics::composer {

FileNode::FileNode(const std::string& nodePath) : mNodePath(nodePath) {}

FileNode::~FileNode() {
    for (auto& fd : mFds) {
        close(fd.second);
    }
}

std::string FileNode::dump() {
    std::ostringstream os;
    os << "FileNode: root path: " << mNodePath << std::endl;
    for (const auto& item : mFds) {
        auto lastWrittenString = getLastWrittenString(item.first);
        if (lastWrittenString)
            os << "FileNode: sysfs node = " << item.first
               << ", last written value = " << *lastWrittenString << std::endl;
    }
    return os.str();
}

std::optional<std::string> FileNode::getLastWrittenString(const std::string& nodeName) {
    int fd = getFileHandler(nodeName);
    if ((fd < 0) || (mLastWrittenString.count(fd) <= 0)) return std::nullopt;
    return mLastWrittenString[fd];
}

std::optional<std::string> FileNode::readString(const std::string& nodeName) {
    std::string fullPath = mNodePath + nodeName;
    std::ifstream ifs(fullPath);
    if (ifs) {
        std::ostringstream os;
        os << ifs.rdbuf(); // reading data
        return os.str();
    }
    return std::nullopt;
}

int FileNode::getFileHandler(const std::string& nodeName) {
    if (mFds.count(nodeName) > 0) {
        return mFds[nodeName];
    }
    std::string fullPath = mNodePath + nodeName;
    int fd = open(fullPath.c_str(), O_WRONLY, 0);
    if (fd < 0) {
        ALOGE("Open file node %s failed, fd = %d", fullPath.c_str(), fd);
        return fd;
    }
    mFds[nodeName] = fd;
    return fd;
}

bool FileNode::writeString(const std::string& nodeName, const std::string& str) {
    int fd = getFileHandler(nodeName);
    if (fd < 0) {
        ALOGE("Write to invalid file node %s%s", mNodePath.c_str(), nodeName.c_str());
        return false;
    }
    int ret = write(fd, str.c_str(), str.size());
    if (ret < 0) {
        ALOGE("Write %s to file node %s%s failed, ret = %d errno = %d", str.c_str(),
              mNodePath.c_str(), nodeName.c_str(), ret, errno);
        return false;
    }
    std::ostringstream oss;
    oss << "Write " << str << " to file node " << mNodePath.c_str() << nodeName.c_str();
    ATRACE_NAME(oss.str().c_str());
    mLastWrittenString[fd] = str;
    return true;
}
}; // namespace hardware::graphics::composer
}; // namespace android
