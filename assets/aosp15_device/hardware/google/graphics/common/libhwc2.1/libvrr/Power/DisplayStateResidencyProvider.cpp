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

#include <stdlib.h>

#include "DisplayStateResidencyProvider.h"

namespace android::hardware::graphics::composer {

namespace {

static constexpr uint64_t MilliToNano = 1000000;

}

DisplayStateResidencyProvider::DisplayStateResidencyProvider(
        std::shared_ptr<CommonDisplayContextProvider> displayContextProvider,
        std::shared_ptr<StatisticsProvider> statisticsProvider)
      : mDisplayContextProvider(displayContextProvider), mStatisticsProvider(statisticsProvider) {
    generatePowerStatsStates();
    mStartStatisticTimeNs = mStatisticsProvider->getStartStatisticTimeNs();
}

void DisplayStateResidencyProvider::getStateResidency(std::vector<StateResidency>* stats) {
    int64_t powerStatsTotalTimeNs = aggregateStatistics();
#ifdef DEBUG_VRR_POWERSTATS
    uint64_t statisticDurationNs = getBootClockTimeNs() - mStartStatisticTimeNs;
    ALOGD("DisplayStateResidencyProvider: total power stats time = %ld ms, time lapse = %ld ms",
          powerStatsTotalTimeNs / MilliToNano, statisticDurationNs / MilliToNano);
    if (mLastGetStateResidencyTimeNs != -1) {
        int64_t timePassedNs = (getSteadyClockTimeNs() - mLastGetStateResidencyTimeNs);
        int64_t statisticAccumulatedTimeNs = (powerStatsTotalTimeNs - mLastPowerStatsTotalTimeNs);
        ALOGD("DisplayStateResidencyProvider: The time interval between successive calls to "
              "getStateResidency() = %ld ms",
              (timePassedNs / MilliToNano));
        ALOGD("DisplayStateResidencyProvider: The accumulated statistic time interval between "
              "successive calls to "
              "getStateResidency() = %ld ms",
              (statisticAccumulatedTimeNs / MilliToNano));
    }
    mLastGetStateResidencyTimeNs = getSteadyClockTimeNs();
    mLastPowerStatsTotalTimeNs = powerStatsTotalTimeNs;
#endif
    *stats = mStateResidency;
}

const std::vector<State>& DisplayStateResidencyProvider::getStates() {
    return mStates;
}

uint64_t DisplayStateResidencyProvider::aggregateStatistics() {
    uint64_t totalTimeNs = 0;
    std::set<int> firstIteration;
    auto updatedStatistics = mStatisticsProvider->getUpdatedStatistics();
    for (auto& statistic : updatedStatistics) {
        auto it = mPowerStatsProfileToIdMap.find(statistic.first.toPowerStatsProfile());
        if (it == mPowerStatsProfileToIdMap.end()) {
            ALOGE("DisplayStateResidencyProvider %s(): unregistered powerstats state [%s]",
                  __func__, statistic.first.toPowerStatsProfile().toString().c_str());
            continue;
        }
        int id = it->second;
        const auto& displayPresentRecord = statistic.second;

        auto& stateResidency = mStateResidency[id];
        if (firstIteration.count(id) > 0) {
            stateResidency.totalStateEntryCount += displayPresentRecord.mCount;
            stateResidency.lastEntryTimestampMs =
                    std::max<uint64_t>(stateResidency.lastEntryTimestampMs,
                                       displayPresentRecord.mLastTimeStampInBootClockNs /
                                               MilliToNano);
            stateResidency.totalTimeInStateMs +=
                    displayPresentRecord.mAccumulatedTimeNs / MilliToNano;
        } else {
            stateResidency.totalStateEntryCount = displayPresentRecord.mCount;
            stateResidency.lastEntryTimestampMs =
                    displayPresentRecord.mLastTimeStampInBootClockNs / MilliToNano;
            stateResidency.totalTimeInStateMs =
                    displayPresentRecord.mAccumulatedTimeNs / MilliToNano;
            firstIteration.insert(id);
        }

        statistic.second.mUpdated = false;
        totalTimeNs += displayPresentRecord.mAccumulatedTimeNs;
    }
    return totalTimeNs;
}

void DisplayStateResidencyProvider::generateUniqueStates() {
    auto configs = mDisplayContextProvider->getDisplayConfigs();
    if (!configs) return; // Early return if no configs

    // Special case: Power mode OFF
    mUniqueStates.emplace(PowerStatsProfile{.mPowerMode = HWC2_POWER_MODE_OFF}, "OFF");

    // Iterate through all combinations
    for (auto refreshSource : android::hardware::graphics::composer::kRefreshSource) {
        for (auto powerMode : android::hardware::graphics::composer::kActivePowerModes) {
            // LPM and NP is not possible. skipping
            if (!isPresentRefresh(refreshSource) && powerMode == HWC2_POWER_MODE_DOZE) {
                continue;
            }
            for (const auto& config : *configs) {
                for (int brightnessMode = static_cast<int>(BrightnessMode::kNormalBrightnessMode);
                     brightnessMode < static_cast<int>(BrightnessMode::kInvalidBrightnessMode);
                     ++brightnessMode) {
                    PowerStatsProfile
                            profile{.mWidth = mDisplayContextProvider->getWidth(config.first),
                                    .mHeight = mDisplayContextProvider->getHeight(config.first),
                                    .mFps = 0, // Initially set to 0
                                    .mPowerMode = powerMode,
                                    .mBrightnessMode = static_cast<BrightnessMode>(brightnessMode),
                                    .mRefreshSource = refreshSource};

                    if (powerMode == HWC_POWER_MODE_DOZE) {
                        for (auto fps :
                             android::hardware::graphics::composer::kFpsLowPowerModeMappingTable) {
                            profile.mFps = fps;
                            mUniqueStates.emplace(profile,
                                                  mPowerStatsProfileTokenGenerator
                                                          .generateStateName(&profile));
                        }
                    } else {
                        mUniqueStates.emplace(profile,
                                              mPowerStatsProfileTokenGenerator.generateStateName(
                                                      &profile));
                        for (auto fps : android::hardware::graphics::composer::kFpsMappingTable) {
                            profile.mFps = fps.round();
                            mUniqueStates.emplace(profile,
                                                  mPowerStatsProfileTokenGenerator
                                                          .generateStateName(&profile));
                        }
                    }
                }
            }
        }
    }
}

void DisplayStateResidencyProvider::generatePowerStatsStates() {
    generateUniqueStates();

    // Sort and assign a unique identifier to each state string.
    std::map<std::string, int> stateNameIDMap;
    int index = 0;
    for (const auto& state : mUniqueStates) {
        auto it = stateNameIDMap.find(state.second);
        int id = index;
        // If the stateName already exists, update mPowerStatsProfileToIdMap, and skip
        // updating mStates/Residency
        if (it != stateNameIDMap.end()) {
            id = it->second;
        } else {
            stateNameIDMap.insert({state.second, id});
            index++;
            mStates.push_back({id, state.second});
            mStateResidency.emplace_back();
            mStateResidency.back().id = id;
        }
        mPowerStatsProfileToIdMap[state.first] = id;
    }

#ifdef DEBUG_VRR_POWERSTATS
    for (const auto& state : mStates) {
        ALOGI("DisplayStateResidencyProvider state id = %d, content = %s, len = %ld", state.id,
              state.name.c_str(), state.name.length());
    }
#endif
}

} // namespace android::hardware::graphics::composer
