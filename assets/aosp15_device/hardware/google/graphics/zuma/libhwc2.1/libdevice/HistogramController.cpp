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

#include "HistogramController.h"

HistogramController::HistogramController(ExynosDisplay* display)
      : HistogramDevice(display, 4, {3}) {
    for (int i = 0; i < mConfigInfos.size(); ++i)
        mConfigInfos[i] = std::make_shared<ConfigInfo>(mPreemptConfigs[i].config);
}

void HistogramController::initPlatformHistogramCapability() {
    mHistogramCapability.supportSamplePosList.push_back(HistogramSamplePos::PRE_POSTPROC);
    mHistogramCapability.supportBlockingRoi = true;

#if defined(EXYNOS_CONTEXT_HISTOGRAM_EVENT_REQUEST)
    SCOPED_HIST_LOCK(mHistogramMutex);
    if ((int)mFreeChannels.size() + (int)mUsedChannels.size() >= (int)mPreemptConfigs.size())
        mHistogramCapability.supportQueryOpr = true;
    else
        mHistogramCapability.supportQueryOpr = false;
#else
    mHistogramCapability.supportQueryOpr = false;
#endif
}

ndk::ScopedAStatus HistogramController::queryOPR(std::array<double, kOPRConfigsCount>& oprVals) {
    ATRACE_CALL();

    if (waitInitDrmDone() == false) {
        HIST_LOG(E, "initDrm is not completed yet");
        // TODO: b/323158344 - add retry error in HistogramErrorCode and return here.
        return ndk::ScopedAStatus::fromExceptionCode(EX_UNSUPPORTED_OPERATION);
    }

    {
        std::shared_lock lock(mHistogramCapabilityMutex);
        if (!mHistogramCapability.supportQueryOpr) {
            HIST_LOG(W, "supportQueryOpr is false");
            return ndk::ScopedAStatus::fromExceptionCode(EX_UNSUPPORTED_OPERATION);
        }
    }

    HistogramErrorCode histogramErrorCode = HistogramErrorCode::NONE;

    const auto [displayActiveH, displayActiveV] = snapDisplayActiveSize();

    std::array<uint32_t, kOPRConfigsCount> blobIds;
    std::array<int, kOPRConfigsCount> channelIds;

    {
        SCOPED_HIST_LOCK(mHistogramMutex);
        if ((histogramErrorCode = getOPRBlobs(blobIds, displayActiveH, displayActiveV)) !=
            HistogramErrorCode::NONE)
            return errorToStatus(histogramErrorCode);

        if ((histogramErrorCode = preemptOPRChannels()) != HistogramErrorCode::NONE)
            return errorToStatus(histogramErrorCode);

        scheduler();
        getOPRChannels(channelIds);
    }

    {
        ATRACE_NAME("HistogramOnRefresh");
        mDisplay->mDevice->onRefresh(mDisplay->mDisplayId);
    }

    histogramErrorCode = getOPRVals(blobIds, channelIds, oprVals);

    bool needRefresh = false;
    {
        // Clear configInfo for OPR request
        SCOPED_HIST_LOCK(mHistogramMutex);
        for (auto& configInfo : mConfigInfos) resetConfigInfoStatus(configInfo);
        needRefresh = scheduler();
    }

    if (needRefresh) {
        ATRACE_NAME("HistogramOnRefresh");
        mDisplay->mDevice->onRefresh(mDisplay->mDisplayId);
    }

    if (histogramErrorCode != HistogramErrorCode::NONE) return errorToStatus(histogramErrorCode);

    return ndk::ScopedAStatus::ok();
}

HistogramController::HistogramErrorCode HistogramController::getOPRVals(
        const std::array<uint32_t, kOPRConfigsCount>& blobIds,
        const std::array<int, kOPRConfigsCount>& channelIds,
        std::array<double, kOPRConfigsCount>& oprVals) {
    ATRACE_CALL();

    std::array<std::vector<char16_t>, kOPRConfigsCount> histogramBuffer;
    std::array<std::cv_status, kOPRConfigsCount> cv_status;
    std::array<HistogramErrorCode, kOPRConfigsCount> errorCodes;

    // Get the moduleDisplayInterface pointer
    ExynosDisplayDrmInterface* moduleDisplayInterface =
            static_cast<ExynosDisplayDrmInterface*>(mDisplay->mDisplayInterface.get());
    if (!moduleDisplayInterface) {
        HIST_LOG(E, "ENABLE_HIST_ERROR, moduleDisplayInterface is NULL");
        return HistogramErrorCode::ENABLE_HIST_ERROR;
    }

    // Use shared_ptr to keep the blobIdData which will be used by receiveBlobIdData and
    // receiveBlobIdData.
    std::array<std::shared_ptr<BlobIdData>, kOPRConfigsCount> blobIdDatas;
    for (int i = 0; i < kOPRConfigsCount; ++i)
        searchOrCreateBlobIdData(blobIds[i], true, blobIdDatas[i]);

    // Request the drmEvent of the blobId, hold only one uniqueLock at a time.
    for (int i = 0; i < kOPRConfigsCount; ++i) {
        std::shared_ptr<BlobIdData>& blobIdData = blobIdDatas[i];
        uint32_t blobId = blobIds[i];
        int channelId = channelIds[i];

        std::unique_lock<std::mutex> lock(blobIdData->mDataCollectingMutex);
        ::android::base::ScopedLockAssertion lock_assertion(blobIdData->mDataCollectingMutex);
        ATRACE_NAME(String8::format("mDataCollectingMutex(blob#%u)", blobId));

        errorCodes[i] = HistogramErrorCode::NONE;
        requestBlobIdData(moduleDisplayInterface, &errorCodes[i], channelId, blobId, blobIdData);
    }

    // Receive the drmEvent of the blobId, hold only one uniqueLock at a time.
    for (int i = 0; i < kOPRConfigsCount; ++i) {
        // If the requestBlobIdData has some error, not need to receive blobIdData.
        if (errorCodes[i] != HistogramErrorCode::NONE) continue;

        std::shared_ptr<BlobIdData>& blobIdData = blobIdDatas[i];

        std::unique_lock<std::mutex> lock(blobIdData->mDataCollectingMutex);
        ::android::base::ScopedLockAssertion lock_assertion(blobIdData->mDataCollectingMutex);
        ATRACE_NAME(String8::format("mDataCollectingMutex(blob#%u)", blobIds[i]));

        cv_status[i] =
                receiveBlobIdData(moduleDisplayInterface, &histogramBuffer[i], &errorCodes[i],
                                  channelIds[i], blobIds[i], blobIdData, lock);
    }

    // Check the query result and clear the buffer if needed (no lock is held now)
    for (int i = 0; i < blobIdDatas.size(); ++i) {
        checkQueryResult(&histogramBuffer[i], &errorCodes[i], channelIds[i], blobIds[i],
                         cv_status[i]);
    }

    // Check the histogram error code
    for (auto& error : errorCodes)
        if (error != HistogramErrorCode::NONE) return error;

    for (int i = 0; i < kOPRConfigsCount; ++i) {
        errorCodes[i] = calculateOPRVal(mPreemptConfigs[i].name, histogramBuffer[i], blobIds[i],
                                        channelIds[i], oprVals[i]);
        if (errorCodes[i] != HistogramErrorCode::NONE) return errorCodes[i];
    }

    return HistogramErrorCode::NONE;
}

HistogramController::HistogramErrorCode HistogramController::calculateOPRVal(
        const char* configName, const std::vector<char16_t>& histogramBuffer, const uint32_t blobId,
        const int channelId, double& oprVal) const {
    if (histogramBuffer.size() != HISTOGRAM_BINS_SIZE) {
        HIST_LOG(E, "BAD_HIST_DATA for %s, buffer size is not %lu", configName,
                 HISTOGRAM_BINS_SIZE);
        return HistogramErrorCode::BAD_HIST_DATA;
    }

    int totalPixels = 0;
    double totalLuma = 0;

    for (int i = 0; i < HISTOGRAM_BINS_SIZE; ++i) {
        // TODO: b/340403552 - More efficient way to convert gamma into linear space.
        totalPixels += histogramBuffer[i];
        totalLuma += std::pow((i / (double)(HISTOGRAM_BINS_SIZE - 1)), 2.2) * histogramBuffer[i];
    }

    if (!totalPixels) {
        HIST_BLOB_CH_LOG(E, blobId, channelId, "BAD_HIST_DATA for %s, pixel count is zero",
                         configName);
        return HistogramErrorCode::BAD_HIST_DATA;
    }

    oprVal = totalLuma / totalPixels;
    HIST_BLOB_CH_LOG(V, blobId, channelId, "%s channel opr: %lf, pixels count: %d", configName,
                     oprVal, totalPixels);
    return HistogramErrorCode::NONE;
}

HistogramController::HistogramErrorCode HistogramController::getOPRBlobs(
        std::array<uint32_t, kOPRConfigsCount>& blobIds, const int displayActiveH,
        const int displayActiveV) {
    ATRACE_CALL();

    // Get blobIds for all mPreemptConfigs
    for (int i = 0; i < kOPRConfigsCount; ++i) {
        auto& configInfo = mConfigInfos[i];
        bool isBlobIdChanged = false;

        uint32_t blobId = getMatchBlobId(configInfo->mBlobsList, displayActiveH, displayActiveV,
                                         isBlobIdChanged);

        // Create the blobId for not matched case (first time queryOPR or RRS detected)
        int ret;
        if (!blobId) {
            std::shared_ptr<PropertyBlob> drmConfigBlob;
            ret = createDrmConfigBlob(configInfo->mRequestedConfig, displayActiveH, displayActiveV,
                                      drmConfigBlob);
            if (ret || !drmConfigBlob) {
                HIST_LOG(E, "createDrmConfigBlob failed, ret(%d)", ret);
                return HistogramErrorCode::CONFIG_HIST_ERROR;
            }

            // Attach the histogram drmConfigBlob to the configInfo
            configInfo->mBlobsList.emplace_front(displayActiveH, displayActiveV, drmConfigBlob);
            blobId = drmConfigBlob->getId();
        }

        blobIds[i] = blobId;
    }

    return HistogramErrorCode::NONE;
}

void HistogramController::getOPRChannels(std::array<int, kOPRConfigsCount>& channelIds) const {
    // Get channelIds
    for (int i = 0; i < kOPRConfigsCount; ++i) channelIds[i] = mConfigInfos[i]->mChannelId;
}

HistogramController::HistogramErrorCode HistogramController::preemptOPRChannels() {
    ATRACE_CALL();

    for (auto& configInfo : mConfigInfos) resetConfigInfoStatus(configInfo);

    int preemptCount = (int)mConfigInfos.size() - (int)mFreeChannels.size();
    if (preemptCount > (int)mUsedChannels.size()) {
        HIST_LOG(E, "no channel available for preemption");
        return HistogramErrorCode::NO_CHANNEL_AVAILABLE;
    }

    for (int i = 0; i < preemptCount; ++i) swapOutConfigInfo(*mUsedChannels.begin());

    for (auto& configInfo : mConfigInfos) addConfigToInactiveList(configInfo, true);

    return HistogramErrorCode::NONE;
}

void HistogramController::postAtomicCommitCleanup() {
    ATRACE_CALL();

    bool needRefresh = false;

    {
        bool needSchedule = false;

        SCOPED_HIST_LOCK(mHistogramMutex);
        for (int i = 0; i < kOPRConfigsCount; ++i) {
            auto& configInfo = mConfigInfos[i];
            if (configInfo->mStatus == ConfigInfo::Status_t::HAS_CHANNEL_ASSIGNED &&
                mChannels[configInfo->mChannelId].mStatus != ChannelStatus_t::CONFIG_PENDING) {
                needSchedule = true;
                resetConfigInfoStatus(configInfo);
            }
        }

        if (needSchedule) needRefresh = scheduler();
    }

    if (needRefresh) {
        ATRACE_NAME("HistogramOnRefresh");
        mDisplay->mDevice->onRefresh(mDisplay->mDisplayId);
    }
}

void HistogramController::dumpInternalConfigs(String8& result) const {
    for (int i = 0; i < kOPRConfigsCount; ++i) {
        result.appendFormat("Histogram internal config \"%s\":\n", mPreemptConfigs[i].name);
        if (mConfigInfos[i]) {
            mConfigInfos[i]->dump(result, "\t");
        } else {
            result.appendFormat("\tconfigInfo: %p\n", mConfigInfos[i].get());
        }
    }
}
