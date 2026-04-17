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
export SERVE_PID=''

# Default environment

export PGHOST="localhost"
export PGPORT="5432"
export PGUSER="postgres"
export PGPASSWORD="postgres"
export PGDATABASE="postgres"

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

x_kill_proc_rmrf_tmp() {
  # stop all processes, clean ALL state
  sudo pkill -9 postgres || true
  sudo pkill -9 pgrwl || true
  sudo rm -rf /tmp/*
}

x_remake_dirs() {
  x_kill_proc_rmrf_tmp

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
}

x_stop_receiver() {
  if [[ -n "${RECEIVER_PID:-}" ]]; then
    log_info "stopping receiver (PID $RECEIVER_PID)"

    ## NOTE: version-1
    ##    
    # kill -TERM "$RECEIVER_PID" 2>/dev/null || true
    # wait "$RECEIVER_PID" 2>/dev/null || true

    ## NOTE: version-2 (single controlled mode)
    # stop receive loop, start serving wal files
    curl -X POST http://127.0.0.1:7070/receiver/states/stopped    
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

x_start_serving() {
  local cfg=$1
  log_info "starting wal-serving with $cfg"

  ## NOTE: version-1
  ##
  # # Run the 'serve' mode in background.
  # #   * stdout  -> tee -> log file (append) -> /dev/null (discard)
  # #   * stderr  -> tee -> log file (append) -> original stderr (so it appears on console)
  # /usr/local/bin/pgrwl daemon -c "${cfg}" -m serve \
  #   > >(tee -a "$LOG_FILE") \
  #   2> >(tee -a "$LOG_FILE" >&2) &

  ## NOTE: version-2 (single controlled mode)
  # stop receive loop, start serving wal files
  curl -X POST http://127.0.0.1:7070/receiver/states/stopped

  SERVE_PID=$!
}

# toxiproxy utils

export TOXIPROXY_API="http://toxiproxy:8474"
export TOXIPROXY_MINIO_PROXY="minio_s3"
export TOXIPROXY_MINIO_LISTEN="0.0.0.0:9005"
export TOXIPROXY_MINIO_UPSTREAM="minio:9000"

x_wait_http_ok() {
  local url="${1:?url required}"
  local timeout="${2:-30}"
  local i

  for ((i = 0; i < timeout; i++)); do
    if curl -fsS "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done

  return 1
}

x_wait_toxiproxy_up() {
  log_info "waiting toxiproxy api"
  x_wait_http_ok "${TOXIPROXY_API}/version" 30
}

x_toxiproxy_reset() {
  log_info "resetting toxiproxy"
  curl -fsS -X POST "${TOXIPROXY_API}/reset" >/dev/null
}

x_toxiproxy_create_minio_proxy() {
  log_info "creating toxiproxy minio proxy"
  curl -fsS -X POST "${TOXIPROXY_API}/proxies" \
    -H 'Content-Type: application/json' \
    -d "{
      \"name\": \"${TOXIPROXY_MINIO_PROXY}\",
      \"listen\": \"${TOXIPROXY_MINIO_LISTEN}\",
      \"upstream\": \"${TOXIPROXY_MINIO_UPSTREAM}\"
    }" >/dev/null || true
}

x_toxiproxy_delete_minio_proxy() {
  curl -fsS -X DELETE "${TOXIPROXY_API}/proxies/${TOXIPROXY_MINIO_PROXY}" >/dev/null 2>&1 || true
}

x_toxiproxy_enable_minio() {
  log_info "enabling minio proxy"
  curl -fsS -X POST "${TOXIPROXY_API}/proxies/${TOXIPROXY_MINIO_PROXY}" \
    -H 'Content-Type: application/json' \
    -d '{"enabled":true}' >/dev/null
}

x_toxiproxy_disable_minio() {
  log_info "disabling minio proxy"
  curl -fsS -X POST "${TOXIPROXY_API}/proxies/${TOXIPROXY_MINIO_PROXY}" \
    -H 'Content-Type: application/json' \
    -d '{"enabled":false}' >/dev/null
}

x_toxiproxy_setup_minio() {
  x_wait_toxiproxy_up
  x_toxiproxy_reset
  x_toxiproxy_delete_minio_proxy
  x_toxiproxy_create_minio_proxy
  x_toxiproxy_enable_minio
}

x_toxiproxy_cut_minio_after_delay() {
  local delay="${1:-2}"
  local downtime="${2:-5}"

  (
    sleep "${delay}"
    log_info "cutting minio through toxiproxy for ${downtime}s"
    x_toxiproxy_disable_minio
    sleep "${downtime}"
    x_toxiproxy_enable_minio
    log_info "restored minio through toxiproxy"
  ) &
}

x_toxiproxy_flap_minio() {
  local delay_before_first="${1:-2}"
  local cycles="${2:-2}"
  local downtime="${3:-4}"
  local pause_between="${4:-3}"

  (
    sleep "${delay_before_first}"
    local i
    for ((i = 1; i <= cycles; i++)); do
      log_info "toxiproxy minio flap cycle ${i}/${cycles}: down ${downtime}s"
      x_toxiproxy_disable_minio
      sleep "${downtime}"
      x_toxiproxy_enable_minio

      if (( i < cycles )); then
        sleep "${pause_between}"
      fi
    done
  ) &
}
