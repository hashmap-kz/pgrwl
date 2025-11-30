#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/pg/pg.sh

TEST_NAME=$(basename "$0" .sh)
TEST_STATE_PATH="/var/lib/postgresql/test-state/${TEST_NAME}"

# Cleanup on exit (even on error)
cleanup() {
  set +e
  # save content for debug
  mkdir -p "${TEST_STATE_PATH}"
  cp -a /tmp/* "${TEST_STATE_PATH}/"
  # cleanup state
  rm -rf /tmp/*
}
trap cleanup EXIT

export BASEBACKUP_PATH="/tmp/basebackup"
export WAL_PATH="/tmp/wal-archive"
export LOG_FILE="/tmp/pgrwl.log"
export PG_RECEIVEWAL_WAL_PATH="/tmp/wal-archive-pg_receivewal"
export PG_RECEIVEWAL_LOG_FILE="/tmp/pg_receivewal.log"
export BACKGROUND_INSERTS_SCRIPT_PATH="/var/lib/postgresql/scripts/gendata/inserts.sh"
export BACKGROUND_INSERTS_SCRIPT_LOG_FILE="/tmp/ts-inserts.log"

# Default environment

export PGHOST="localhost"
export PGPORT="5432"
export PGUSER="postgres"
export PGPASSWORD="postgres"

# cleanup possible state

x_remake_buckets() {
  minio-mc alias set local https://minio:9000 minioadmin minioadmin123 --insecure
  minio-mc rb --force local/backups --insecure || true

  # Wait until bucket is really gone
  for i in {1..10}; do
    if minio-mc ls local/backups --insecure >/dev/null 2>&1; then
      log_info "Waiting for bucket to be deleted..."
      sleep 1
    else
      log_info "Bucket is deleted."
      break
    fi
  done

  minio-mc mb local/backups --insecure || true
  minio-mc version enable local/backups --insecure
}

x_remake_dirs() {
  # stop all processes, clean ALL state
  sudo pkill -9 postgres || true
  sudo pkill -9 pgrwl || true
  sudo rm -rf /tmp/*

  # recreate localFS
  rm -rf "${BASEBACKUP_PATH}" && mkdir -p "${BASEBACKUP_PATH}"
  rm -rf "${WAL_PATH}" && mkdir -p "${WAL_PATH}"
  rm -rf "${PG_RECEIVEWAL_WAL_PATH}" && mkdir -p "${PG_RECEIVEWAL_WAL_PATH}"
  chown -R postgres:postgres "${PG_RECEIVEWAL_WAL_PATH}"

  # recreate bucket
  x_remake_buckets
}
