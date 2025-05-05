#!/bin/bash

APP_NAME="pg_receivewal"
APP_PATH="pg_receivewal"
WAL_PATH="/tmp/wal-archive-pg_receivewal"

ARGS=(
  "-D" "${WAL_PATH}"
  "-S" "pg_receivewal"
  "--no-loop"
  "--verbose"
  "--no-password"
  "--synchronous"
  "--dbname" "dbname=replication options=-cdatestyle=iso replication=true application_name=pg_receivewal"
)

# Slot creation (optional, only for pg_receivewal)
pg_receivewal --no-password --slot=pg_receivewal --create-slot --if-not-exists

source "/var/lib/postgresql/scripts/pg/run_app.sh" "$@"
