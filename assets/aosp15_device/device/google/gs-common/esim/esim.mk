## Configuration for eSIM bootstrap

# Enable eSIM bootstrap by default
PRODUCT_PRODUCT_PROPERTIES += setupwizard.feature.provisioning_profile_mode=true

# init.rc and sepolcy to conditionally disable eSIM bootstrap
PRODUCT_PACKAGES += init.esim-gs.rc
BOARD_VENDOR_SEPOLICY_DIRS += device/google/gs-common/esim/sepolicy/vendor
SYSTEM_EXT_PUBLIC_SEPOLICY_DIRS += device/google/gs-common/esim/sepolicy/system_ext/public
SYSTEM_EXT_PRIVATE_SEPOLICY_DIRS += device/google/gs-common/esim/sepolicy/system_ext/private
