!/bin/sh

# Copyright 2024 The Android Open Source Project
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

source ../../../common/clear-factory-images-variables.sh
BUILD=12249137
DEVICE=tegu
PRODUCT=tegu
VERSION=bd4a.240820.002
SRCPREFIX=signed-
BOOTLOADER=tegu-15.2-12242320
RADIO=g5300t-240809-240812-b-12214855
source ../../../common/generate-factory-images-common.sh
