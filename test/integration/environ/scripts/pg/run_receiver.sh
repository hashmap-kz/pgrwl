#!/bin/bash

APP_NAME="x05"
APP_PATH="/usr/local/bin/x05"
WAL_PATH="/tmp/wal-archive"

ARGS=(
  "-D" "${WAL_PATH}"
  "-S" "pg_recval_5"
  "--log-add-source"
  "--no-loop"
  "--log-level" "debug"
)

source "/var/lib/postgresql/scripts/pg/run_app.sh" "$@"
