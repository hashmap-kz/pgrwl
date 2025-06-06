#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/pg/pg.sh

export BASEBACKUP_PATH="/tmp/basebackup"
export WAL_PATH="/tmp/wal-archive"
export WAL_PATH_PG_RECEIVEWAL="/tmp/wal-archive-pg_receivewal"
export LOG_FILE="/tmp/pgrwl.log"

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
  pkill -9 postgres || true
  pkill -9 pgrwl || true
  rm -rf "/tmp/*"

  # recreate localFS
  rm -rf "${BASEBACKUP_PATH}" && mkdir -p "${BASEBACKUP_PATH}"
  rm -rf "${WAL_PATH}" && mkdir -p "${WAL_PATH}"
  rm -rf "${WAL_PATH_PG_RECEIVEWAL}" && mkdir -p "${WAL_PATH_PG_RECEIVEWAL}"

  # recreate bucket
  x_remake_buckets
}
