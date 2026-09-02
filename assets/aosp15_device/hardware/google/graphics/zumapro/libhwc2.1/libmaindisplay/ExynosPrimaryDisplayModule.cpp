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

#define ATRACE_TAG (ATRACE_TAG_GRAPHICS | ATRACE_TAG_HAL)

#include <android/binder_ibinder.h>
#include <android/binder_status.h>
#include <cutils/properties.h>

#include "ExynosHWCHelper.h"
#include "ExynosPrimaryDisplayModule.h"

#define DISP_STR(disp) (disp)->mDisplayName.c_str()

#define OP_MANAGER_LOGI(disp, msg, ...) \
    ALOGI("[%s] OperationRateManager::%s:" msg, DISP_STR(disp), __func__, ##__VA_ARGS__)
#define OP_MANAGER_LOGW(disp, msg, ...) \
    ALOGW("[%s] OperationRateManager::%s:" msg, DISP_STR(disp), __func__, ##__VA_ARGS__)
#define OP_MANAGER_LOGE(disp, msg, ...) \
    ALOGE("[%s] OperationRateManager::%s:" msg, DISP_STR(disp), __func__, ##__VA_ARGS__)

static constexpr int64_t kQueryPeriodNanosecs = std::chrono::nanoseconds(100ms).count();

using namespace zumapro;

ExynosPrimaryDisplayModule::ExynosPrimaryDisplayModule(uint32_t index, ExynosDevice* device,
                                                       const std::string& displayName)
      : gs201::ExynosPrimaryDisplayModule(index, device, displayName) {
    int32_t hs_hz = property_get_int32("vendor.primarydisplay.op.hs_hz", 0);
    int32_t ns_hz = property_get_int32("vendor.primarydisplay.op.ns_hz", 0);

    if (hs_hz && ns_hz) {
        mOperationRateManager = std::make_unique<OperationRateManager>(this, hs_hz, ns_hz);
    }
}

ExynosPrimaryDisplayModule::~ExynosPrimaryDisplayModule() {}

int32_t ExynosPrimaryDisplayModule::validateWinConfigData() {
    return ExynosDisplay::validateWinConfigData();
}

ExynosPrimaryDisplayModule::OperationRateManager::OperationRateManager(
        ExynosPrimaryDisplay* display, int32_t hsHz, int32_t nsHz)
      : gs201::ExynosPrimaryDisplayModule::OperationRateManager(),
        mDisplay(display),
        mDisplayHsOperationRate(hsHz),
        mDisplayNsOperationRate(nsHz),
        mDisplayPeakRefreshRate(0),
        mDisplayRefreshRate(0),
        mDisplayLastDbv(0),
        mDisplayDbv(0),
        mDisplayHsSwitchMinDbv(0),
        mDisplayPowerMode(HWC2_POWER_MODE_ON),
        mDisplayLowBatteryModeEnabled(false),
        mHistogramQueryWorker(nullptr) {
    mDisplayNsMinDbv = property_get_int32("vendor.primarydisplay.op.ns_min_dbv", 0);
    mDisplayTargetOperationRate = mDisplayHsOperationRate;
    OP_MANAGER_LOGI(mDisplay, "Op Rate: NS=%d HS=%d NsMinDbv=%d", mDisplayNsOperationRate,
                    mDisplayHsOperationRate, mDisplayNsMinDbv);

    float histDeltaTh =
            static_cast<float>(property_get_int32("vendor.primarydisplay.op.hist_delta_th", 0));
    if (histDeltaTh) {
        mHistogramQueryWorker = std::make_unique<HistogramQueryWorker>(this, histDeltaTh);
        mDisplayHsSwitchMinDbv =
                property_get_int32("vendor.primarydisplay.op.hs_switch_min_dbv", 0);
    }
}

ExynosPrimaryDisplayModule::OperationRateManager::~OperationRateManager() {}

int32_t ExynosPrimaryDisplayModule::OperationRateManager::getTargetOperationRate() const {
    if (mDisplayPowerMode == HWC2_POWER_MODE_DOZE ||
        mDisplayPowerMode == HWC2_POWER_MODE_DOZE_SUSPEND) {
        return kLowPowerOperationRate;
    } else {
        return mDisplayTargetOperationRate;
    }
}

int32_t ExynosPrimaryDisplayModule::OperationRateManager::onPeakRefreshRate(uint32_t rate) {
    char rateStr[PROP_VALUE_MAX];
    std::sprintf(rateStr, "%d", rate);

    DISPLAY_STR_LOGD(DISP_STR(mDisplay), eDebugOperationRate, "OperationRateManager: rate=%d",
                     rate);

    Mutex::Autolock lock(mLock);
    if (property_set("persist.vendor.primarydisplay.op.peak_refresh_rate", rateStr) < 0) {
        OP_MANAGER_LOGE(mDisplay,
                        "failed to set property persist.primarydisplay.op.peak_refresh_rate");
    }
    mDisplayPeakRefreshRate = rate;
    return 0;
}

int32_t ExynosPrimaryDisplayModule::OperationRateManager::onLowPowerMode(bool enabled) {
    DISPLAY_STR_LOGD(DISP_STR(mDisplay), eDebugOperationRate, "enabled=%d", enabled);

    Mutex::Autolock lock(mLock);
    mDisplayLowBatteryModeEnabled = enabled;
    return 0;
}

int32_t ExynosPrimaryDisplayModule::OperationRateManager::onConfig(hwc2_config_t cfg) {
    Mutex::Autolock lock(mLock);
    int32_t targetRefreshRate = mDisplay->getRefreshRate(cfg);
    if (mHistogramQueryWorker && mHistogramQueryWorker->isRuntimeResolutionConfig() &&
        mDisplayRefreshRate == targetRefreshRate) {
        mHistogramQueryWorker->updateConfig(mDisplay->mXres, mDisplay->mYres);
        // skip op update for Runtime Resolution config
        return 0;
    }
    mDisplayRefreshRate = targetRefreshRate;
    DISPLAY_STR_LOGD(DISP_STR(mDisplay), eDebugOperationRate, "OperationRateManager: rate=%d",
                     mDisplayRefreshRate);
    updateOperationRateLocked(DispOpCondition::SET_CONFIG);
    return 0;
}

int32_t ExynosPrimaryDisplayModule::OperationRateManager::onBrightness(uint32_t dbv) {
    Mutex::Autolock lock(mLock);
    if (dbv == 0 || mDisplayLastDbv == dbv) return 0;
    DISPLAY_STR_LOGD(DISP_STR(mDisplay), eDebugOperationRate, "OperationRateManager: dbv=%d", dbv);
    mDisplayDbv = dbv;

    /*
        Update peak_refresh_rate from persist/vendor prop after a brightness change.
        1. Otherwise there will be NS-HS-NS switch during the onPowerMode.
        2. When constructor is called, persist property is not ready yet and returns 0.
    */
    if (!mDisplayPeakRefreshRate) {
        char rateStr[PROP_VALUE_MAX];
        int32_t vendorPeakRefreshRate = 0, persistPeakRefreshRate = 0;
        if (property_get("persist.vendor.primarydisplay.op.peak_refresh_rate", rateStr, "0") >= 0 &&
            atoi(rateStr) > 0) {
            persistPeakRefreshRate = atoi(rateStr);
            mDisplayPeakRefreshRate = persistPeakRefreshRate;
        } else {
            vendorPeakRefreshRate =
                    property_get_int32("vendor.primarydisplay.op.peak_refresh_rate", 0);
            mDisplayPeakRefreshRate = vendorPeakRefreshRate;
        }

        DISPLAY_STR_LOGD(DISP_STR(mDisplay), eDebugOperationRate,
                         "OperationRateManager: peak_refresh_rate=%d[vendor: %d|persist %d]",
                         mDisplayPeakRefreshRate, vendorPeakRefreshRate, persistPeakRefreshRate);
    }

    return updateOperationRateLocked(DispOpCondition::SET_DBV);
}

int32_t ExynosPrimaryDisplayModule::OperationRateManager::onPowerMode(int32_t mode) {
    std::string modeName = "Unknown";
    if (mode == HWC2_POWER_MODE_ON) {
        modeName = "On";
    } else if (mode == HWC2_POWER_MODE_OFF) {
        modeName = "Off";
    } else if (mode == HWC2_POWER_MODE_DOZE || mode == HWC2_POWER_MODE_DOZE_SUSPEND) {
        modeName = "LP";
    }

    DISPLAY_STR_LOGD(DISP_STR(mDisplay), eDebugOperationRate, "OperationRateManager: mode=%s",
                     modeName.c_str());

    Mutex::Autolock lock(mLock);
    mDisplayPowerMode = static_cast<hwc2_power_mode_t>(mode);
    return updateOperationRateLocked(DispOpCondition::PANEL_SET_POWER);
}

int32_t ExynosPrimaryDisplayModule::OperationRateManager::onHistogram() {
    Mutex::Autolock lock(mLock);
    DISPLAY_STR_LOGD(DISP_STR(mDisplay), eDebugOperationRate,
                     "histogram reach to the luma delta threshold");
    return updateOperationRateLocked(DispOpCondition::HISTOGRAM_DELTA);
}

int32_t ExynosPrimaryDisplayModule::OperationRateManager::updateOperationRateLocked(
        const DispOpCondition cond) {
    int32_t ret = HWC2_ERROR_NONE, dbv;

    ATRACE_CALL();
    if (cond == DispOpCondition::SET_DBV) {
        dbv = mDisplayDbv;
    } else {
        dbv = mDisplayLastDbv;
    }

    int32_t desiredOpRate = mDisplayHsOperationRate;
    bool isSteadyLowRefreshRate =
            (mDisplayPeakRefreshRate && mDisplayPeakRefreshRate <= mDisplayNsOperationRate) ||
            mDisplayLowBatteryModeEnabled;
    bool isDbvInBlockingZone = mDisplayLowBatteryModeEnabled ? (dbv < mDisplayNsMinDbv)
                                                             : (dbv < mDisplayHsSwitchMinDbv);
    int32_t effectiveOpRate = 0;

    // check minimal operation rate needed
    if (isSteadyLowRefreshRate && mDisplayRefreshRate <= mDisplayNsOperationRate) {
        desiredOpRate = mDisplayNsOperationRate;
    }
    // check blocking zone
    if (isDbvInBlockingZone) {
        DISPLAY_STR_LOGD(DISP_STR(mDisplay), eDebugOperationRate,
                         "OperationRateManager: in blocking zone (dbv %d, min %d)", dbv,
                         mDisplayNsMinDbv);
        desiredOpRate = mDisplayHsOperationRate;
    }

    if (mDisplayPowerMode == HWC2_POWER_MODE_DOZE ||
        mDisplayPowerMode == HWC2_POWER_MODE_DOZE_SUSPEND) {
        mDisplayTargetOperationRate = kLowPowerOperationRate;
        desiredOpRate = mDisplayTargetOperationRate;
        effectiveOpRate = desiredOpRate;
    } else if (mDisplayPowerMode != HWC2_POWER_MODE_ON) {
        if (mHistogramQueryWorker) {
            DISPLAY_STR_LOGD(DISP_STR(mDisplay), eDebugOperationRate,
                             "histogram stopQuery due to power off");
            mHistogramQueryWorker->stopQuery();
        }
        return ret;
    }

    if (cond == DispOpCondition::SET_CONFIG) {
        if (mDisplayRefreshRate <= mDisplayHsOperationRate) {
            if (!mHistogramQueryWorker) {
                if (mDisplayRefreshRate > mDisplayNsOperationRate) {
                    effectiveOpRate = mDisplayHsOperationRate;
                }
            } else {
                if (mDisplayRefreshRate == mDisplayTargetOperationRate && !isDbvInBlockingZone) {
                    DISPLAY_STR_LOGD(DISP_STR(mDisplay), eDebugOperationRate,
                                     "histogram stopQuery due to the same config");
                    mHistogramQueryWorker->stopQuery();
                }
                if (!isDbvInBlockingZone) {
                    if (mDisplayLowBatteryModeEnabled &&
                        (!mDisplayHsSwitchMinDbv || dbv < mDisplayHsSwitchMinDbv)) {
                        // delay the switch of NS->HS until conditions are satisfied
                        desiredOpRate = mDisplayRefreshRate;
                    } else if (mDisplayRefreshRate > mDisplayNsOperationRate) {
                        // will switch to HS immediately
                        effectiveOpRate = mDisplayHsOperationRate;
                    }
                }
            }
        }
    } else if (cond == DispOpCondition::PANEL_SET_POWER) {
        if (mDisplayPowerMode == HWC2_POWER_MODE_ON) {
            mDisplayTargetOperationRate = getTargetOperationRate();
        }
        effectiveOpRate = desiredOpRate;
    } else if (cond == DispOpCondition::SET_DBV) {
        // TODO: tune brightness delta for different brightness curve and values
        int32_t delta = abs(dbv - mDisplayLastDbv);
        if (!mHistogramQueryWorker) {
            if (desiredOpRate == mDisplayHsOperationRate || delta > kBrightnessDeltaThreshold) {
                effectiveOpRate = desiredOpRate;
            }
        } else {
            if (delta > kBrightnessDeltaThreshold) {
                effectiveOpRate = desiredOpRate;
                DISPLAY_STR_LOGD(DISP_STR(mDisplay), eDebugOperationRate,
                                 "histogram stopQuery due to dbv delta");
                mHistogramQueryWorker->stopQuery();
            }
        }
        mDisplayLastDbv = dbv;
        if (effectiveOpRate > kLowPowerOperationRate &&
            (effectiveOpRate != mDisplayTargetOperationRate)) {
            DISPLAY_STR_LOGD(DISP_STR(mDisplay), eDebugOperationRate,
                             "OperationRateManager: brightness delta=%d", delta);
        } else {
            if (!mHistogramQueryWorker ||
                (desiredOpRate == mDisplayNsOperationRate && isDbvInBlockingZone)) {
                return ret;
            }
        }
    } else if (cond == DispOpCondition::HISTOGRAM_DELTA) {
        effectiveOpRate = desiredOpRate;
    }

    if (!mDisplay->isConfigSettingEnabled() && effectiveOpRate == mDisplayNsOperationRate) {
        OP_MANAGER_LOGI(mDisplay, "rate switching is disabled, skip NS op rate update");
        return ret;
    } else if (effectiveOpRate > kLowPowerOperationRate &&
               effectiveOpRate != mDisplayTargetOperationRate) {
        mDisplayTargetOperationRate = effectiveOpRate;
        OP_MANAGER_LOGI(mDisplay, "set target operation rate %d", effectiveOpRate);
    }

    if (mHistogramQueryWorker && mDisplayTargetOperationRate != desiredOpRate) {
        DISPLAY_STR_LOGD(DISP_STR(mDisplay), eDebugOperationRate, "histogram startQuery");
        mHistogramQueryWorker->startQuery();
    }

    OP_MANAGER_LOGI(mDisplay,
                    "Target@%d(desired:%d) | Refresh@%d(peak:%d), Battery:%s, DBV:%d(NsMin:%d, "
                    "HsSwitchMin:%d)",
                    mDisplayTargetOperationRate, desiredOpRate, mDisplayRefreshRate,
                    mDisplayPeakRefreshRate, mDisplayLowBatteryModeEnabled ? "Low" : "OK",
                    mDisplayLastDbv, mDisplayNsMinDbv, mDisplayHsSwitchMinDbv);
    return ret;
}

void ExynosPrimaryDisplayModule::checkPreblendingRequirement() {
    if (!hasDisplayColor()) {
        DISPLAY_LOGD(eDebugTDM, "%s is skipped because of no displaycolor", __func__);
        return;
    }

    String8 log;
    int count = 0;

    auto checkPreblending = [&](const int idx, ExynosMPPSource* mppSrc) -> int {
        auto* colorManager = getColorManager();
        if (!colorManager) return false;
        auto& dpp = colorManager->getDppForLayer(mppSrc);
        mppSrc->mNeedPreblending =
                dpp.EotfLut().enable || dpp.Gm().enable || dpp.Dtm().enable || dpp.OetfLut().enable;
        if (hwcCheckDebugMessages(eDebugTDM)) {
            log.appendFormat(" i=%d,pb(%d-%d,%d,%d,%d)", idx, mppSrc->mNeedPreblending,
                             dpp.EotfLut().enable, dpp.Gm().enable, dpp.Dtm().enable,
                             dpp.OetfLut().enable);
        }
        return mppSrc->mNeedPreblending;
    };

    // for client target
    count += checkPreblending(-1, &mClientCompositionInfo);

    // for normal layers
    for (size_t i = 0; i < mLayers.size(); ++i) {
        count += checkPreblending(i, mLayers[i]);
    }
    DISPLAY_LOGD(eDebugTDM, "disp(%d),cnt=%d%s", mDisplayId, count, log.c_str());
}

ExynosPrimaryDisplayModule::OperationRateManager::HistogramQueryWorker::HistogramQueryWorker(
        OperationRateManager* opRateManager, float deltaThreshold)
      : Worker("HistogramQueryWorker", HAL_PRIORITY_URGENT_DISPLAY),
        mOpRateManager(opRateManager),
        mSpAIBinder(nullptr),
        mReady(false),
        mQueryMode(false),
        mHistogramLumaDeltaThreshold(deltaThreshold),
        mPrevHistogramLuma(0) {
    InitWorker();
}

ExynosPrimaryDisplayModule::OperationRateManager::HistogramQueryWorker::~HistogramQueryWorker() {
    unprepare();
    mReady = false;
    Exit();
}

void* stub_OnCreate(void*) {
    return nullptr;
}

void stub_OnDestroy(void*) {}

binder_status_t stub_OnTransact(AIBinder*, transaction_code_t, const AParcel*, AParcel*) {
    return STATUS_OK;
}

void ExynosPrimaryDisplayModule::OperationRateManager::HistogramQueryWorker::prepare() {
    AIBinder_Class* binderClass = AIBinder_Class_define("disp_op_query_worker", stub_OnCreate,
                                                        stub_OnDestroy, stub_OnTransact);
    AIBinder* aibinder = AIBinder_new(binderClass, nullptr);
    mSpAIBinder.set(aibinder);

    if (mSpAIBinder.get()) {
        // assign (0, 0, 0, 0) to indicate full screen roi since display probably isn't ready
        mConfig.roi.left = 0;
        mConfig.roi.top = 0;
        mConfig.roi.right = 0;
        mConfig.roi.bottom = 0;
        mConfig.weights.weightR = kHistogramConfigWeightR;
        mConfig.weights.weightG = kHistogramConfigWeightG;
        mConfig.weights.weightB = kHistogramConfigWeightB;
        mConfig.samplePos = HistogramDevice::HistogramSamplePos::POST_POSTPROC;

        HistogramDevice::HistogramErrorCode err = HistogramDevice::HistogramErrorCode::NONE;
        ndk::ScopedAStatus status =
                mOpRateManager->mDisplay->mHistogramController->registerHistogram(mSpAIBinder,
                                                                                  mConfig, &err);
        if (!status.isOk()) {
            OP_MANAGER_LOGE(mOpRateManager->mDisplay, "failed to register histogram (binder err)");
            return;
        }
        if (err != HistogramDevice::HistogramErrorCode::NONE) {
            OP_MANAGER_LOGE(mOpRateManager->mDisplay, "failed to register histogram (hist err)");
            return;
        }
    } else {
        OP_MANAGER_LOGE(mOpRateManager->mDisplay, "failed to get binder for histogram");
        return;
    }

    // assign panel resolution for isRuntimeResolutionConfig()
    mConfig.roi.right = mOpRateManager->mDisplay->mXres;
    mConfig.roi.bottom = mOpRateManager->mDisplay->mYres;
    mReady = true;
    OP_MANAGER_LOGI(mOpRateManager->mDisplay, "register histogram successfully");
}

void ExynosPrimaryDisplayModule::OperationRateManager::HistogramQueryWorker::unprepare() {
    if (!mReady) return;

    HistogramDevice::HistogramErrorCode err = HistogramDevice::HistogramErrorCode::NONE;
    mOpRateManager->mDisplay->mHistogramController->unregisterHistogram(mSpAIBinder, &err);
    if (err != HistogramDevice::HistogramErrorCode::NONE) {
        OP_MANAGER_LOGE(mOpRateManager->mDisplay, "failed to unregister histogram");
    }
}

bool ExynosPrimaryDisplayModule::OperationRateManager::HistogramQueryWorker::
        isRuntimeResolutionConfig() const {
    if (!mReady) return false;

    if (mConfig.roi.right == mOpRateManager->mDisplay->mXres ||
        mConfig.roi.bottom == mOpRateManager->mDisplay->mYres) {
        return false;
    }

    // histogram will change roi automatically, no need to reconfig
    DISPLAY_STR_LOGD(DISP_STR(mOpRateManager->mDisplay), eDebugOperationRate,
                     "histogram %dx%d->%dx%d", mConfig.roi.right, mConfig.roi.bottom,
                     mOpRateManager->mDisplay->mXres, mOpRateManager->mDisplay->mYres);
    return true;
}

void ExynosPrimaryDisplayModule::OperationRateManager::HistogramQueryWorker::updateConfig(
        uint32_t xres, uint32_t yres) {
    mConfig.roi.right = xres;
    mConfig.roi.bottom = yres;
}

void ExynosPrimaryDisplayModule::OperationRateManager::HistogramQueryWorker::startQuery() {
    if (!mReady) return;

    Signal();
}

void ExynosPrimaryDisplayModule::OperationRateManager::HistogramQueryWorker::stopQuery() {
    mQueryMode = false;
}

void ExynosPrimaryDisplayModule::OperationRateManager::HistogramQueryWorker::Routine() {
    if (!mOpRateManager->mDisplay->mHistogramController) return;

    if (!mReady) {
        prepare();
        return;
    }

    int ret;
    // WaitForSignalOrExitLocked() needs to be enclosed by Lock() and Unlock()
    Lock();
    if (!mQueryMode) {
        DISPLAY_STR_LOGD(DISP_STR(mOpRateManager->mDisplay), eDebugOperationRate,
                         "histogram wait for signal");
        ret = WaitForSignalOrExitLocked();
        mQueryMode = true;
        mPrevHistogramLuma = 0;
    } else {
        ret = WaitForSignalOrExitLocked(kQueryPeriodNanosecs);
    }
    if (ret == -EINTR) {
        OP_MANAGER_LOGE(mOpRateManager->mDisplay, "histogram failed to wait for signal");
        mQueryMode = false;
        Unlock();
        return;
    }
    Unlock();

    HistogramDevice::HistogramErrorCode err = HistogramDevice::HistogramErrorCode::NONE;
    std::vector<char16_t> data;
    ndk::ScopedAStatus status =
            mOpRateManager->mDisplay->mHistogramController->queryHistogram(mSpAIBinder, &data,
                                                                           &err);
    if (status.isOk() && err != HistogramDevice::HistogramErrorCode::BAD_TOKEN) {
        if (!data.size()) {
            OP_MANAGER_LOGW(mOpRateManager->mDisplay, "histogram data is empty");
            return;
        }

        float lumaSum = 0;
        int count = 0;
        for (int i = 0; i < data.size(); i++) {
            lumaSum += i * static_cast<int>(data[i]);
            count += static_cast<int>(data[i]);
        }
        if (!count) {
            OP_MANAGER_LOGW(mOpRateManager->mDisplay, "histogram count is 0");
            return;
        }

        float luma = lumaSum / count;
        float lumaDelta = abs(luma - mPrevHistogramLuma);
        DISPLAY_STR_LOGD(DISP_STR(mOpRateManager->mDisplay), eDebugOperationRate,
                         "histogram luma %f, delta %f, th %f", luma, lumaDelta,
                         mHistogramLumaDeltaThreshold);
        if (mPrevHistogramLuma && lumaDelta > mHistogramLumaDeltaThreshold) {
            mQueryMode = false;
            mOpRateManager->onHistogram();
            mOpRateManager->mDisplay->handleTargetOperationRate();
        }
        mPrevHistogramLuma = luma;
    } else {
        OP_MANAGER_LOGE(mOpRateManager->mDisplay, "histogram failed to query");
    }
}
