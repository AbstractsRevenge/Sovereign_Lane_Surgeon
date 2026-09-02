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

#include <optional>
#include <string>

#include "../display/common/CommonDisplayContextProvider.h"
#include "PowerStatsProfile.h"

namespace android::hardware::graphics::composer {

struct StateNameComparator {
    bool operator()(const std::string& a, const std::string& b) const {
        // 1. Find the last '@' in both strings
        size_t posA = a.rfind('@');
        size_t posB = b.rfind('@');

        // 2. Extract the parts before and after the '@'
        std::string prefixA = (posA != std::string::npos) ? a.substr(0, posA) : a;
        std::string suffixA = (posA != std::string::npos) ? a.substr(posA + 1) : "";
        std::string prefixB = (posB != std::string::npos) ? b.substr(0, posB) : b;
        std::string suffixB = (posB != std::string::npos) ? b.substr(posB + 1) : "";

        // 3. Compare prefixes first
        if (prefixA != prefixB) {
            return prefixA < prefixB;
        }

        // 4. If prefixes are the same, check for "np" and extract numeric parts
        bool hasNpA = suffixA.find("np") == 0;
        bool hasNpB = suffixB.find("np") == 0;
        std::string numPartA = hasNpA ? suffixA.substr(2) : suffixA;
        std::string numPartB = hasNpB ? suffixB.substr(2) : suffixB;

        // 5. Compare based on "np" presence
        if (hasNpA != hasNpB) {
            return !hasNpA; // "np" prefixes come after non-"np" prefixes
        }

        // 6. If both have "np" or neither has it, compare numeric parts
        bool isNumA = std::all_of(numPartA.begin(), numPartA.end(), ::isdigit);
        bool isNumB = std::all_of(numPartB.begin(), numPartB.end(), ::isdigit);

        if (isNumA && isNumB) {
            char* endPtrA;
            char* endPtrB;

            long numA = strtol(numPartA.c_str(), &endPtrA, 10);
            long numB = strtol(numPartB.c_str(), &endPtrB, 10);

            if (*endPtrA != '\0' || *endPtrB != '\0' || numA < std::numeric_limits<int>::min() ||
                numA > std::numeric_limits<int>::max() || numB < std::numeric_limits<int>::min() ||
                numB > std::numeric_limits<int>::max()) {
                ALOGE("Error parsing numeric parts in KeyComparator");

                return false;
            }

            return numA < numB;
        } else {
            return suffixA < suffixB;
        }
    }
};

class PowerStatsProfileTokenGenerator {
public:
    PowerStatsProfileTokenGenerator();

    std::optional<std::string> generateToken(const std::string& tokenLabel,
                                             PowerStatsProfile* profile);

    std::string generateStateName(PowerStatsProfile* profile, bool enableMapping = true);

private:
    // The format of pattern is: ([token label]'delimiter'?)*
    static constexpr std::string_view kPresentDisplayStateResidencyPattern =
            "[mode](:)[width](x)[height](@)[fps]()";

    // The format of pattern is: ([token label]'delimiter'?)*
    static constexpr std::string_view kNonPresentDisplayStateResidencyPattern =
            "[mode](:)[width](x)[height](@)[refreshSource]()";

    static constexpr char kTokenLabelStart = '[';
    static constexpr char kTokenLabelEnd = ']';
    static constexpr char kDelimiterStart = '(';
    static constexpr char kDelimiterEnd = ')';

    bool parseDisplayStateResidencyPattern();

    bool parseResidencyPattern(
            std::vector<std::pair<std::string, std::string>>& residencyPatternMap,
            const std::string_view residencyPattern);

    std::string generateRefreshSourceToken(PowerStatsProfile* profile) const;

    std::string generateModeToken(PowerStatsProfile* profile) const;

    std::string generateWidthToken(PowerStatsProfile* profile) const;

    std::string generateHeightToken(PowerStatsProfile* profile) const;

    std::string generateFpsToken(PowerStatsProfile* profile) const;

    std::vector<std::pair<std::string, std::string>> mNonPresentDisplayStateResidencyPatternList;
    std::vector<std::pair<std::string, std::string>> mPresentDisplayStateResidencyPatternList;
};

} // namespace android::hardware::graphics::composer
