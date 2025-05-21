#!/bin/bash

APP_NAME="pgrwl"
APP_PATH="/usr/local/bin/pgrwl"
WAL_PATH="/tmp/wal-archive"

ARGS=(
  "start"
  "-c" "/var/lib/postgresql/configs/01-basic-receive.json"
  "-m" "serve"
)

mkdir -p "${WAL_PATH}"
chown -R postgres:postgres "${WAL_PATH}"

source "/var/lib/postgresql/scripts/pg/run_app.sh" "$@"
