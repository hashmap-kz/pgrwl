#!/bin/bash

APP_NAME="pgrwl"
APP_PATH="/usr/local/bin/pgrwl"
WAL_PATH="/tmp/wal-archive"

ARGS=(
  "-D" "${WAL_PATH}"
  "-S" "pg_recwal_5"
  "--log-add-source"
  "--no-loop"
  "--log-level" "debug"
)

mkdir -p "${WAL_PATH}"
chown -R postgres:postgres "${WAL_PATH}"

# Default environment
export PGHOST="${PGHOST:-localhost}"
export PGPORT="${PGPORT:-5432}"
export PGUSER="${PGUSER:-postgres}"
export PGPASSWORD="${PGPASSWORD:-postgres}"

source "/var/lib/postgresql/scripts/pg/run_app.sh" "$@"
