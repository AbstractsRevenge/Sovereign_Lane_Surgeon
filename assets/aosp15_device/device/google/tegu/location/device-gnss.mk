# include common gnss binaries
-include vendor/samsung_slsi/gps/s5300/gnss_release.mk

# include customized gps config files
PRODUCT_COPY_FILES += \
    device/google/tegu/location/ca.pem:vendor/etc/gnss/ca.pem

ifneq (,$(filter userdebug eng, $(TARGET_BUILD_VARIANT)))
    PRODUCT_COPY_FILES += \
        device/google/tegu/location/gps.cfg:vendor/etc/gnss/gps.cfg
    PRODUCT_VENDOR_PROPERTIES += \
        vendor.gps.aol.enabled=true
else
    PRODUCT_COPY_FILES += \
        device/google/tegu/location/gps_user.cfg:vendor/etc/gnss/gps.cfg
endif

# include pixel gnss hal service
-include vendor/google_devices/zumapro/proprietary/gps/pixel_gnss_hal.mk

# sepolicy
BOARD_VENDOR_SEPOLICY_DIRS += device/google/gs-common/gps/lsi/sepolicy
