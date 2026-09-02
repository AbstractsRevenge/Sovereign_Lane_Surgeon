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

#pragma once

#include <hardware/hwcomposer2.h>
#include <map>
#include <sstream>
#include <string>
#include <utility>

#include "../Power/PowerStatsProfile.h"
#include "../Power/PowerStatsProfileTokenGenerator.h"
#include "EventQueue.h"
#include "Utils.h"
#include "display/common/CommonDisplayContextProvider.h"
#include "display/common/Constants.h"
#include "interface/DisplayContextProvider.h"
#include "interface/VariableRefreshRateInterface.h"

// #define DEBUG_VRR_STATISTICS 1

namespace android::hardware::graphics::composer {

// |DisplayStatus| is the intrinsic property of the key for statistics, representing the display
// configuration.
typedef struct DisplayStatus {
    inline bool isOff() const {
        if ((mPowerMode == HWC_POWER_MODE_OFF) || (mPowerMode == HWC_POWER_MODE_DOZE_SUSPEND)) {
            return true;
        } else {
            return false;
        }
    }

    bool operator==(const DisplayStatus& rhs) const {
        if (isOff() || rhs.isOff()) {
            return isOff() == rhs.isOff();
        }
        return (mActiveConfigId == rhs.mActiveConfigId) && (mPowerMode == rhs.mPowerMode) &&
                (mBrightnessMode == rhs.mBrightnessMode);
    }

    bool operator<(const DisplayStatus& rhs) const {
        if (isOff() && rhs.isOff()) {
            return false;
        }

        if (mPowerMode != rhs.mPowerMode) {
            return (isOff() || (mPowerMode < rhs.mPowerMode));
        } else if (mActiveConfigId != rhs.mActiveConfigId) {
            return mActiveConfigId < rhs.mActiveConfigId;
        } else {
            return mBrightnessMode < rhs.mBrightnessMode;
        }
    }

    std::string toString() const {
        std::ostringstream os;
        os << "id = " << mActiveConfigId;
        os << ", power mode = " << mPowerMode;
        os << ", brightness = " << static_cast<int>(mBrightnessMode);
        return os.str();
    }

    hwc2_config_t mActiveConfigId = -1;
    int mPowerMode = HWC_POWER_MODE_OFF;
    BrightnessMode mBrightnessMode = BrightnessMode::kInvalidBrightnessMode;
} DisplayStatus;

// |DisplayRefreshProfile| is the key to the statistics.
typedef struct DisplayRefreshProfile {
    PowerStatsProfile toPowerStatsProfile(bool enableMapping = true) const {
        PowerStatsProfile powerStatsProfile;
        if (mNumVsync < 0) { // To address the specific scenario of powering off
            powerStatsProfile.mFps = -1;
            return powerStatsProfile;
        }
        powerStatsProfile.mWidth = mWidth;
        powerStatsProfile.mHeight = mHeight;
        powerStatsProfile.mPowerMode = mCurrentDisplayConfig.mPowerMode;
        powerStatsProfile.mBrightnessMode = mCurrentDisplayConfig.mBrightnessMode;
        powerStatsProfile.mRefreshSource = mRefreshSource;
        Fraction fps(mTeFrequency, mNumVsync);
        if (enableMapping) {
            if ((android::hardware::graphics::composer::kFpsMappingTable.count(fps) > 0)) {
                powerStatsProfile.mFps = fps.round();
            } else {
                powerStatsProfile.mFps = 0;
            }
        } else {
            powerStatsProfile.mFps = fps.round();
        }

        return powerStatsProfile;
    }

    inline bool isOff() const { return mCurrentDisplayConfig.isOff(); }

    bool operator<(const DisplayRefreshProfile& rhs) const {
        if (isOff() || rhs.isOff()) {
            if (isOff() == rhs.isOff()) {
                return false;
            }
        }

        if (mCurrentDisplayConfig != rhs.mCurrentDisplayConfig) {
            return (mCurrentDisplayConfig < rhs.mCurrentDisplayConfig);
        } else if (mNumVsync != rhs.mNumVsync) {
            return (mNumVsync < rhs.mNumVsync);
        } else {
            return (mRefreshSource < rhs.mRefreshSource);
        }
    }

    std::string toString() const {
        std::string res = mCurrentDisplayConfig.toString();
        res += ", mNumVsync = " + std::to_string(mNumVsync) + ", refresh source = " +
                (isPresentRefresh(mRefreshSource) ? "present" : "nonpresent");
        return res;
    }

    DisplayStatus mCurrentDisplayConfig;
    int mTeFrequency;
    int mWidth = 0;
    int mHeight = 0;
    // |mNumVsync| is the timing property of the key for statistics, representing the distribution
    // of refreshs. It represents the interval between a refresh and the previous refresh in
    // terms of the number of vsyncs.
    int mNumVsync = -1;
    RefreshSource mRefreshSource = kRefreshSourceActivePresent;
} DisplayRefreshProfile;

// |DisplayRefreshRecord| is the value to the statistics.
typedef struct DisplayRefreshRecord {
    DisplayRefreshRecord() = default;
    DisplayRefreshRecord& operator+=(const DisplayRefreshRecord& other) {
        this->mCount += other.mCount;
        this->mAccumulatedTimeNs += other.mAccumulatedTimeNs;
        this->mLastTimeStampInBootClockNs =
                std::max(mLastTimeStampInBootClockNs, other.mLastTimeStampInBootClockNs);
        mUpdated = true;
        return *this;
    }
    std::string toString() const {
        std::ostringstream os;
        os << "Count = " << mCount;
        os << ", AccumulatedTime Ms = " << mAccumulatedTimeNs / 1000000;
        os << ", LastTimeStampInBootClockNs = " << mLastTimeStampInBootClockNs;
        return os.str();
    }
    uint64_t mCount = 0;
    uint64_t mAccumulatedTimeNs = 0;
    uint64_t mLastTimeStampInBootClockNs = 0;
    bool mUpdated = false;
} DisplayRefreshRecord;

// |DisplayRefreshStatistics| is a map consisting of key-value pairs for statistics.
// The key consists of two parts: display configuration and refresh frequency (in terms of vsync).
typedef std::map<DisplayRefreshProfile, DisplayRefreshRecord> DisplayRefreshStatistics;

class StatisticsProvider {
public:
    virtual ~StatisticsProvider() = default;

    virtual uint64_t getStartStatisticTimeNs() const = 0;

    virtual DisplayRefreshStatistics getStatistics() = 0;

    virtual DisplayRefreshStatistics getUpdatedStatistics() = 0;
};

class VariableRefreshRateStatistic : public PowerModeListener,
                                     public RefreshListener,
                                     public StatisticsProvider {
public:
    VariableRefreshRateStatistic(CommonDisplayContextProvider* displayContextProvider,
                                 EventQueue* eventQueue, int maxFrameRate, int maxTeFrequency,
                                 int64_t updatePeriodNs);

    uint64_t getPowerOffDurationNs() const;

    uint64_t getStartStatisticTimeNs() const override;

    DisplayRefreshStatistics getStatistics() override;

    DisplayRefreshStatistics getUpdatedStatistics() override;

    void onPowerStateChange(int from, int to) final;

    void onPresent(int64_t presentTimeNs, int flag) override;

    void onNonPresentRefresh(int64_t refreshTimeNs, RefreshSource refreshSource) override;

    void setActiveVrrConfiguration(int activeConfigId, int teFrequency);

    // If |minimumRefreshRate| is not equal to zero, enforce the minimum (fixed) refresh rate;
    // otherwise, revert to a variable refresh rate.
    void setFixedRefreshRate(uint32_t minimumRefreshRate);

    VariableRefreshRateStatistic(const VariableRefreshRateStatistic& other) = delete;
    VariableRefreshRateStatistic& operator=(const VariableRefreshRateStatistic& other) = delete;

    std::string dumpStatistics(bool getUpdatedOnly, RefreshSource refreshSource,
                               const std::string& delimiter = ";");
    void dump(String8& result, const std::vector<std::string>& args = {});

private:
    static constexpr int64_t kMaxRefreshIntervalNs = std::nano::den;
    static constexpr uint32_t kFrameRateWhenPresentAtLpMode = 30;

    bool isPowerModeOffNowLocked() const;

    std::string normalizeString(const std::string& input);

    void onRefreshInternal(int64_t refreshTimeNs, int flag, RefreshSource refreshSource);

    void updateCurrentDisplayStatus();

    void updateIdleStats(int64_t endTimeStampInBootClockNs = -1);

#ifdef DEBUG_VRR_STATISTICS
    int updateStatistic();
#endif

    PowerStatsProfileTokenGenerator mPowerStatsProfileTokenGenerator;

    CommonDisplayContextProvider* mDisplayContextProvider;
    EventQueue* mEventQueue;

    const int mMaxFrameRate;
    const int mMaxTeFrequency;
    const int64_t mMinFrameIntervalNs;

    int mTeFrequency;
    int64_t mTeIntervalNs;

    const int64_t mUpdatePeriodNs;

    int64_t mLastDumpsysTime = 0;
    int64_t mLastRefreshTimeInBootClockNs = kDefaultInvalidPresentTimeNs;

    DisplayRefreshStatistics mStatistics;
    DisplayRefreshStatistics mStatisticsSnapshot;
    DisplayRefreshProfile mDisplayRefreshProfile;

    uint64_t mPowerOffDurationNs = 0;

    uint32_t mMinimumRefreshRate = 1;
    uint64_t mMaximumFrameIntervalNs = kMaxRefreshIntervalNs; // 1 second.

    uint64_t mStartStatisticTimeNs;

#ifdef DEBUG_VRR_STATISTICS
    VrrControllerEvent mUpdateEvent;
#endif

    mutable std::mutex mMutex;
};

} // namespace android::hardware::graphics::composer
