#!/usr/bin/env bash

# logs

logdate() { date "+%Y-%m-%d %H:%M:%S"; }

log_fatal() {
  echo "$(logdate) -- [FATAL] -- $*" >&2
  exit 1
}

log_error() {
  echo "$(logdate) -- [ERROR] -- $*" >&2
}

log_warn() {
  echo "$(logdate) -- [WARNING] -- $*" >&2
}

log_info() {
  echo "$(logdate) -- [INFO] -- $*" >&2
}

log_debug() {
  echo "$(logdate) -- [DEBUG] -- $*" >&2
}

echo_delim() {
  echo ""
  echo "######################################################################"
  echo "### $*"
  echo "######################################################################"
  echo ""
}

# timing utils

x_float_abs() {
  awk -v x="$1" 'BEGIN { if (x < 0) x = -x; printf "%.3f", x }'
}

x_float_gt() {
  awk -v a="$1" -v b="$2" 'BEGIN { exit !(a > b) }'
}

x_float_sub() {
  awk -v a="$1" -v b="$2" 'BEGIN { printf "%.3f", a - b }'
}

x_now_epoch_ms() {
  date +%s.%3N
}
