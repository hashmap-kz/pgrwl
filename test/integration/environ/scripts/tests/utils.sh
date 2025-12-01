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
export LOG_LEVEL_DEFAULT=info
export PG_RECEIVEWAL_WAL_PATH="/tmp/wal-archive-pg_receivewal"
export PG_RECEIVEWAL_LOG_FILE="/tmp/pg_receivewal.log"
export BACKGROUND_INSERTS_SCRIPT_PATH="/var/lib/postgresql/scripts/gendata/inserts.sh"
export BACKGROUND_INSERTS_SCRIPT_LOG_FILE="/tmp/ts-inserts.log"
export RECEIVER_PID=''
export PGRECEIVEWAL_PID=''

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

# start the receiver in background and store its PID
x_start_receiver() {
  local cfg=$1
  log_info "starting receiver with $cfg"

  # Run the receiver in background.
  #   * stdout  -> tee -> log file (append) -> /dev/null (discard)
  #   * stderr  -> tee -> log file (append) -> original stderr (so it appears on console)
  /usr/local/bin/pgrwl daemon -c "${cfg}" -m receive \
    > >(tee -a "$LOG_FILE") \
    2> >(tee -a "$LOG_FILE" >&2) &

  RECEIVER_PID=$!

  # wait until the receiver reports "started" (simple poll)
  for i in {1..30}; do
    if grep -q "wal-receiver started" "$LOG_FILE"; then
      log_info "receiver started (PID $RECEIVER_PID)"
      return
    fi
    sleep 1
  done
  log_error "receiver did not start within timeout"
  kill -9 "$RECEIVER_PID" || true
  exit 1
}

x_stop_receiver() {
  if [[ -n "${RECEIVER_PID:-}" ]]; then
    log_info "stopping receiver (PID $RECEIVER_PID)"
    kill -TERM "$RECEIVER_PID" 2>/dev/null || true
    wait "$RECEIVER_PID" 2>/dev/null || true
  fi
}

# start pg_receivewal in background and store its PID
x_start_pg_receivewal() {
  log_info "starting pg_receivewal"
  pg_receivewal \
    -D "${PG_RECEIVEWAL_WAL_PATH}" \
    -S pg_receivewal \
    --no-loop \
    --verbose \
    --no-password \
    --synchronous \
    --dbname "dbname=replication options=-cdatestyle=iso replication=true application_name=pg_receivewal" \
    >>"${PG_RECEIVEWAL_LOG_FILE}" 2>&1 &
  PGRECEIVEWAL_PID=$!
}

x_stop_pg_receivewal() {
  if [[ -n "${PGRECEIVEWAL_PID:-}" ]]; then
    log_info "stopping pg_receivewal (PID $PGRECEIVEWAL_PID)"
    kill -TERM "$PGRECEIVEWAL_PID" 2>/dev/null || true
    wait "$PGRECEIVEWAL_PID" 2>/dev/null || true
  fi
}

x_generate_wal() {
  local count=${1:-5}
  log_info "generating $count WAL switches"
  for ((i = 0; i < count; i++)); do
    psql -U postgres -c 'DROP TABLE IF EXISTS xxx; SELECT pg_switch_wal(); CREATE TABLE IF NOT EXISTS xxx(id serial);' \
      >/dev/null 2>&1
  done
}
