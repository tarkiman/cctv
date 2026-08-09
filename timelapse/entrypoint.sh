#!/bin/bash
set -e

{
  echo "TIMELAPSE_STORAGE=${TIMELAPSE_STORAGE:-/storage/timelapse}"
  echo "TIMELAPSE_CAMERAS=${TIMELAPSE_CAMERAS}"
  echo "TIMELAPSE_FRAMERATE=${TIMELAPSE_FRAMERATE:-24}"
  echo "TIMELAPSE_KEEP_RAW=${TIMELAPSE_KEEP_RAW:-false}"
  echo "5 0 * * * root cd /app && python3 compile_timelapse.py >> /var/log/timelapse-compile.log 2>&1"
} > /etc/cron.d/timelapse
chmod 0644 /etc/cron.d/timelapse
touch /var/log/timelapse-compile.log

cron

exec python3 capture.py
