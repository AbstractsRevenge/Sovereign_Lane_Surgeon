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

#include <set>

namespace android::hardware::graphics::composer {

inline const std::set<Fraction<int>> kFpsMappingTable = {{240, 240}, {240, 120}, {240, 24},
                                                         {240, 10},  {240, 8},   {240, 7},
                                                         {240, 6},   {240, 5},   {240, 4},
                                                         {240, 3},   {240, 2}};

inline const std::vector<int> kFpsLowPowerModeMappingTable = {1, 30};

const std::vector<int> kActivePowerModes = {HWC2_POWER_MODE_DOZE, HWC2_POWER_MODE_ON};

const std::vector<RefreshSource> kRefreshSource = {kRefreshSourceActivePresent,
                                                   kRefreshSourceIdlePresent,
                                                   kRefreshSourceFrameInsertion,
                                                   kRefreshSourceBrightness};
} // namespace android::hardware::graphics::composer
