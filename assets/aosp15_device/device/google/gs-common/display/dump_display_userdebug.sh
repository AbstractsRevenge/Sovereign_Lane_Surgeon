#!/vendor/bin/sh
display_trace_path="/sys/kernel/tracing/instances/pixel-display/trace"
echo "------ Display Trace ($display_trace_path)------"
cat $display_trace_path

echo "------ HWC Fence States ------"
for f in $(ls /data/vendor/log/hwc/*_hwc_fence_state*.txt)
do
  echo $f
  cat $f
done

echo "------ HWC Error Logs ------"
for f in $(ls /data/vendor/log/hwc/*_hwc_error_log*.txt)
do
  echo $f
  cat $f
done

echo "------ HWC Debug Dumps ------"
for f in $(ls /data/vendor/log/hwc/*_hwc_debug*.dump)
do
  echo $f
  cat $f
done

