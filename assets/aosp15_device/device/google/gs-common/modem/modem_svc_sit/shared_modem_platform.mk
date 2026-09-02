# This file is not included in `modem.mk` since this is included at the
# beginning of each `device.mk` file, and so will be called before
# `SHARED_MODEM_PLATFORM_VENDOR` is defined later in the file.
# This file supoorts Whitechapel(gs101), Whitechapel Pro(gs201), Zuma, Zuma Pro.
# This file doesn't support devices which AP is after Zuma Pro.
# For device after Zuma Pro please use gs-common/modem/shared_modem_platform/shared_modem_platform.mk
SOONG_CONFIG_NAMESPACES += shared_modem_platform
SOONG_CONFIG_shared_modem_platform += \
       vendor
SOONG_CONFIG_shared_modem_platform_vendor := $(SHARED_MODEM_PLATFORM_VENDOR)

PRODUCT_PACKAGES += shared_modem_platform
DEVICE_PRODUCT_COMPATIBILITY_MATRIX_FILE += device/google/gs-common/modem/modem_svc_sit/compatibility_matrix.xml
BOARD_SEPOLICY_DIRS += device/google/gs-common/modem/modem_svc_sit/sepolicy
