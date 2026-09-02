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

#include "HistogramDevice.h"

class HistogramController : public HistogramDevice {
public:
    HistogramController(ExynosDisplay* display);
    virtual void initPlatformHistogramCapability()
            EXCLUDES(mHistogramMutex, mBlobIdDataMutex) override;
    virtual ndk::ScopedAStatus queryOPR(std::array<double, kOPRConfigsCount>& oprVals)
            EXCLUDES(mInitDrmDoneMutex, mHistogramMutex, mBlobIdDataMutex) override;

private:
    struct PreemptConfigInfo {
        const char* name;
        const HistogramConfig config;
    };

    const std::array<PreemptConfigInfo, kOPRConfigsCount> mPreemptConfigs{
        {{
             .name = "Linear space OPR(RED)",
             .config =
                     {
                         .roi = DISABLED_ROI,
                         .weights = {WEIGHT_SUM, 0, 0},
                         .samplePos = HistogramSamplePos::POST_POSTPROC,
                     },
         },
         {
             .name = "Linear space OPR(GREEN)",
             .config =
                     {
                         .roi = DISABLED_ROI,
                         .weights = {0, WEIGHT_SUM, 0},
                         .samplePos = HistogramSamplePos::POST_POSTPROC,
                     },
         },
         {
             .name = "Linear space OPR(BLUE)",
             .config =
                     {
                         .roi = DISABLED_ROI,
                         .weights = {0, 0, WEIGHT_SUM},
                         .samplePos = HistogramSamplePos::POST_POSTPROC,
                     },
         }}};
    std::array<std::shared_ptr<ConfigInfo>, kOPRConfigsCount> mConfigInfos
            GUARDED_BY(mHistogramMutex);

    HistogramErrorCode getOPRVals(const std::array<uint32_t, kOPRConfigsCount>& blobIds,
                                  const std::array<int, kOPRConfigsCount>& channelIds,
                                  std::array<double, kOPRConfigsCount>& oprVals)
            EXCLUDES(mInitDrmDoneMutex, mHistogramMutex, mBlobIdDataMutex);

    HistogramErrorCode calculateOPRVal(const char* configName,
                                       const std::vector<char16_t>& histogramBuffer,
                                       const uint32_t blobId, const int channelId,
                                       double& oprVal) const
            EXCLUDES(mInitDrmDoneMutex, mHistogramMutex, mBlobIdDataMutex);

    HistogramErrorCode getOPRBlobs(std::array<uint32_t, kOPRConfigsCount>& blobIds,
                                   const int displayActiveH, const int displayActiveV)
            REQUIRES(mHistogramMutex) EXCLUDES(mInitDrmDoneMutex, mBlobIdDataMutex);

    void getOPRChannels(std::array<int, kOPRConfigsCount>& channelIds) const
            REQUIRES(mHistogramMutex) EXCLUDES(mInitDrmDoneMutex, mBlobIdDataMutex);

    HistogramErrorCode preemptOPRChannels() REQUIRES(mHistogramMutex)
            EXCLUDES(mInitDrmDoneMutex, mBlobIdDataMutex);

    void postAtomicCommitCleanup() override
            EXCLUDES(mHistogramMutex, mInitDrmDoneMutex, mBlobIdDataMutex);

    void dumpInternalConfigs(String8& result) const override REQUIRES(mHistogramMutex)
            EXCLUDES(mInitDrmDoneMutex, mBlobIdDataMutex);
};
