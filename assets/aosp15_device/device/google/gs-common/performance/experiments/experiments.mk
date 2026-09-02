BOARD_VENDOR_SEPOLICY_DIRS += device/google/gs-common/performance/experiments/sepolicy

PRODUCT_PACKAGES += pixel-experiments-recovery.sh

include device/google/gs-common/performance/experiments/kswapd/kswapd.mk
include device/google/gs-common/performance/experiments/apf/apf.mk
