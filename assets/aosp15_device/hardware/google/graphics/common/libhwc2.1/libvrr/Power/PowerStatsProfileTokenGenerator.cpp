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

#include "PowerStatsProfileTokenGenerator.h"

#include <string>
#include <unordered_map>

namespace android::hardware::graphics::composer {

PowerStatsProfileTokenGenerator::PowerStatsProfileTokenGenerator() {
    parseDisplayStateResidencyPattern();
}

std::string PowerStatsProfileTokenGenerator::generateRefreshSourceToken(
        PowerStatsProfile* profile) const {
    if (profile->isOff()) {
        return "";
    }

    if (isPresentRefresh(profile->mRefreshSource)) {
        return "p";
    } else {
        return "np";
    }
}

std::string PowerStatsProfileTokenGenerator::generateModeToken(PowerStatsProfile* profile) const {
    if (profile->isOff()) {
        return "OFF";
    } else {
        if (profile->mPowerMode == HWC_POWER_MODE_DOZE) {
            return "LPM";
        }
        return (profile->mBrightnessMode == BrightnessMode::kHighBrightnessMode) ? "HBM" : "NBM";
    }
}

std::string PowerStatsProfileTokenGenerator::generateWidthToken(PowerStatsProfile* profile) const {
    if (profile->isOff()) {
        return "";
    }
    return std::to_string(profile->mWidth);
}

std::string PowerStatsProfileTokenGenerator::generateHeightToken(PowerStatsProfile* profile) const {
    if (profile->isOff()) {
        return "";
    }
    return std::to_string(profile->mHeight);
}

std::string PowerStatsProfileTokenGenerator::generateFpsToken(PowerStatsProfile* profile) const {
    if (profile->isOff()) {
        return "";
    }
    if (profile->mFps == 0) {
        return "oth";
    }
    return std::to_string(profile->mFps);
}

std::optional<std::string> PowerStatsProfileTokenGenerator::generateToken(
        const std::string& tokenLabel, PowerStatsProfile* profile) {
    static std::unordered_map<std::string, std::function<std::string(PowerStatsProfile*)>>
            functors = {{"refreshSource",
                         std::bind(&PowerStatsProfileTokenGenerator::generateRefreshSourceToken,
                                   this, std::placeholders::_1)},
                        {"mode",
                         std::bind(&PowerStatsProfileTokenGenerator::generateModeToken, this,
                                   std::placeholders::_1)},
                        {"width",
                         std::bind(&PowerStatsProfileTokenGenerator::generateWidthToken, this,
                                   std::placeholders::_1)},
                        {"height",
                         std::bind(&PowerStatsProfileTokenGenerator::generateHeightToken, this,
                                   std::placeholders::_1)},
                        {"fps",
                         std::bind(&PowerStatsProfileTokenGenerator::generateFpsToken, this,
                                   std::placeholders::_1)}};

    if (functors.find(tokenLabel) != functors.end()) {
        return (functors[tokenLabel])(profile);
    } else {
        ALOGE("%s syntax error: unable to find token label = %s", __func__, tokenLabel.c_str());
        return std::nullopt;
    }
}

std::string PowerStatsProfileTokenGenerator::generateStateName(PowerStatsProfile* profile,
                                                               bool enableMapping) {
    std::string stateName;
    const std::vector<std::pair<std::string, std::string>>& residencyPattern =
            !isPresentRefresh(profile->mRefreshSource) ? mNonPresentDisplayStateResidencyPatternList
                                                       : mPresentDisplayStateResidencyPatternList;

    for (const auto& pattern : residencyPattern) {
        const auto token = generateToken(pattern.first, profile);
        if (token.has_value()) {
            stateName += token.value();
            if (pattern.first == "mode" && token.value() == "OFF") {
                break;
            }
        } else {
            ALOGE("DisplayStateResidencyProvider %s(): cannot find token with label %s", __func__,
                  pattern.first.c_str());
            continue;
        }
        stateName += pattern.second;
    }
    if (!enableMapping && !isPresentRefresh(profile->mRefreshSource)) {
        stateName += generateFpsToken(profile);
    }
    return stateName;
}

bool PowerStatsProfileTokenGenerator::parseResidencyPattern(
        std::vector<std::pair<std::string, std::string>>& residencyPatternMap,
        const std::string_view residencyPattern) {
    size_t start, end;
    start = 0;
    end = -1;
    while (true) {
        start = residencyPattern.find_first_of(kTokenLabelStart, end + 1);
        if (start == std::string::npos) {
            break;
        }
        ++start;
        end = residencyPattern.find_first_of(kTokenLabelEnd, start);
        if (end == std::string::npos) {
            break;
        }
        std::string tokenLabel(residencyPattern.substr(start, end - start));

        start = residencyPattern.find_first_of(kDelimiterStart, end + 1);
        if (start == std::string::npos) {
            break;
        }
        ++start;
        end = residencyPattern.find_first_of(kDelimiterEnd, start);
        if (end == std::string::npos) {
            break;
        }
        std::string delimiter(residencyPattern.substr(start, end - start));
        residencyPatternMap.emplace_back(std::make_pair(tokenLabel, delimiter));
    }
    return (end == residencyPattern.length() - 1);
}

bool PowerStatsProfileTokenGenerator::parseDisplayStateResidencyPattern() {
    return parseResidencyPattern(mPresentDisplayStateResidencyPatternList,
                                 kPresentDisplayStateResidencyPattern) &&
            parseResidencyPattern(mNonPresentDisplayStateResidencyPatternList,
                                  kNonPresentDisplayStateResidencyPattern);
}

} // namespace android::hardware::graphics::composer
