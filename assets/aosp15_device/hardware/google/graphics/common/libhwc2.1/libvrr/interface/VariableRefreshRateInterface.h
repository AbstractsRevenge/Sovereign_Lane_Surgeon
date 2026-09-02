/*
 * Copyright (C) 2023 The Android Open Source Project
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

#pragma once

namespace android::hardware::graphics::composer {

enum RefreshSource {
    // Refresh triggered by presentation.
    kRefreshSourceActivePresent = (1 << 0),
    kRefreshSourceIdlePresent = (1 << 1),
    // Refresh NOT triggered by presentation.
    kRefreshSourceFrameInsertion = (1 << 2),
    kRefreshSourceBrightness = (1 << 3),
};

static constexpr int kRefreshSourcePresentMask =
        kRefreshSourceActivePresent | kRefreshSourceIdlePresent;

static constexpr int kRefreshSourceNonPresentMask =
        kRefreshSourceFrameInsertion | kRefreshSourceBrightness;

class RefreshListener {
public:
    virtual ~RefreshListener() = default;

    virtual void setExpectedPresentTime(int64_t __unused timestampNanos,
                                        int __unused frameIntervalNs) {}

    virtual void onPresent(int32_t __unused fence) {}

    virtual void onPresent(int64_t __unused presentTimeNs, int __unused flag) {}

    virtual void onNonPresentRefresh(int64_t __unused refreshTimeNs,
                                     RefreshSource __unused source) {}
};

class VsyncListener {
public:
    virtual ~VsyncListener() = default;

    virtual void onVsync(int64_t timestamp, int32_t vsyncPeriodNanos) = 0;
};

class PowerModeListener {
public:
    virtual ~PowerModeListener() = default;

    virtual void onPowerStateChange(int from, int to) = 0;
};

class RefreshRateChangeListener {
public:
    virtual ~RefreshRateChangeListener() = default;

    virtual void onRefreshRateChange(int refreshRate) = 0;
};

} // namespace android::hardware::graphics::composer
