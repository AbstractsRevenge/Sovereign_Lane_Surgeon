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

#ifndef EXYNOS_DISPLAY_MODULE_ZUMAPRO_H
#define EXYNOS_DISPLAY_MODULE_ZUMAPRO_H

#include "../../zuma/libhwc2.1/libmaindisplay/ExynosPrimaryDisplayModule.h"
#include "HistogramController.h"
#include "worker.h"

namespace zumapro {

using namespace displaycolor;

class ExynosPrimaryDisplayModule : public gs201::ExynosPrimaryDisplayModule {
public:
    ExynosPrimaryDisplayModule(uint32_t index, ExynosDevice* device,
                               const std::string& displayName);
    ~ExynosPrimaryDisplayModule();
    int32_t validateWinConfigData() override;
    void checkPreblendingRequirement() override;

protected:
    class OperationRateManager : public gs201::ExynosPrimaryDisplayModule::OperationRateManager {
    public:
        OperationRateManager(ExynosPrimaryDisplay* display, int32_t hsHz, int32_t nsHz);
        virtual ~OperationRateManager();

        int32_t onLowPowerMode(bool enabled) override;
        int32_t onPeakRefreshRate(uint32_t rate) override;
        int32_t onConfig(hwc2_config_t cfg) override;
        int32_t onBrightness(uint32_t dbv) override;
        int32_t onPowerMode(int32_t mode) override;
        int32_t getTargetOperationRate() const override;

    protected:
        class HistogramQueryWorker : public Worker {
        public:
            HistogramQueryWorker(OperationRateManager* op, float deltaThreshold);
            ~HistogramQueryWorker();

            bool isRuntimeResolutionConfig() const;
            void updateConfig(uint32_t xres, uint32_t yres);
            void startQuery();
            void stopQuery();

        protected:
            void Routine() override;

        private:
            void prepare();
            void unprepare();

            OperationRateManager* mOpRateManager;
            ndk::SpAIBinder mSpAIBinder;
            HistogramDevice::HistogramConfig mConfig;
            bool mReady;
            bool mQueryMode;
            float mHistogramLumaDeltaThreshold;
            float mPrevHistogramLuma;

            // Use the fixed weights from sensor team's measurement (b/286330225). These values
            // can be used for all devices since we just need a fix set then the DTE team can
            // determine the threshold of luma delta after evaluations.
            static constexpr uint32_t kHistogramConfigWeightR = 186;
            static constexpr uint32_t kHistogramConfigWeightG = 766;
            static constexpr uint32_t kHistogramConfigWeightB = 72;
        };

    private:
        enum class DispOpCondition : uint32_t {
            PANEL_SET_POWER = 0,
            SET_CONFIG,
            SET_DBV,
            HISTOGRAM_DELTA,
            MAX,
        };

        int32_t onHistogram();
        int32_t updateOperationRateLocked(const DispOpCondition cond);

        ExynosPrimaryDisplay* mDisplay;
        const int32_t mDisplayHsOperationRate;
        const int32_t mDisplayNsOperationRate;
        int32_t mDisplayTargetOperationRate;
        int32_t mDisplayNsMinDbv;
        int32_t mDisplayPeakRefreshRate;
        int32_t mDisplayRefreshRate;
        int32_t mDisplayLastDbv;
        int32_t mDisplayDbv;
        int32_t mDisplayHsSwitchMinDbv;
        std::optional<hwc2_power_mode_t> mDisplayPowerMode;
        bool mDisplayLowBatteryModeEnabled;
        Mutex mLock;

        static constexpr uint32_t kBrightnessDeltaThreshold = 10;
        static constexpr uint32_t kLowPowerOperationRate = 30;

        std::unique_ptr<HistogramQueryWorker> mHistogramQueryWorker;
    };
};

} // namespace zumapro

#endif // EXYNOS_DISPLAY_MODULE_ZUMAPRO_H
