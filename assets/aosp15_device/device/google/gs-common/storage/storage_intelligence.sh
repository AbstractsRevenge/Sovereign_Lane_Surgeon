#!/vendor/bin/sh
#
# The script belongs to the feature of UFS FFU via OTA: go/p23-ffu-ota
# Its purpose is to copy the corresponding firmware into partition for UFS FFU.


property="persist.vendor.intelligence"
partition="/dev/block/by-name/userdata_exp.ai"
mount_point="/data/vendor/intelligence"
backend_file="/data/userdata_exp.ai"

request=`getprop $property`

if [ "$request" == "on" ]; then
	mounted=`grep $mount_point /proc/mounts`
	if [ "$mounted" ]; then
		setprop $property on
	else
		setprop $property remove
	fi
elif [ "$request" == "off" ]; then
	dd if=/dev/zero of=$partition bs=4k count=2
	setprop $property remove
fi
