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
