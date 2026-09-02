BOARD_VENDOR_SEPOLICY_DIRS += \
	device/google/gs-common/storage/sepolicy \
	device/google/gs-common/storage/sepolicy/tracking_denials

PRODUCT_PACKAGES += dump_storage

# Pixel storage tool
PRODUCT_PACKAGES_DEBUG += \
	sg_write_buffer \
	sg_read_buffer
