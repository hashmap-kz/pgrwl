#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/tests/utils.sh

# Test: combined mode
#
# Verifies that a single pgrwl process running in 'combined' mode can:
#
#   1. Start with the WAL receiver initially stopped (auto_start: false)
#   2. Start WAL streaming via  POST /receiver {"state":"running"}
#   3. Serve WAL files for restore_command while also receiving -
#      no separate 'serve' process is needed
#   4. Stop WAL streaming via   POST /receiver {"state":"stopped"}
#      while the HTTP server keeps running (restore_command still works)
#   5. Restart WAL streaming via POST /receiver {"state":"running"} again
#   6. Survive a full backup → destroy → restore → WAL-replay cycle

COMBINED_PORT=7070
COMBINED_ADDR="127.0.0.1:${COMBINED_PORT}"
COMBINED_PID=""

x_remake_config() {
  cat <<EOF > "/tmp/config.json"
{
  "main": {
    "listen_port": ${COMBINED_PORT},
    "directory": "/tmp/wal-archive"
  },
  "receiver": {
    "slot": "pgrwl_v5",
    "no_loop": true
  },
  "combined": {
    "auto_start": false
  },
  "log": {
    "level": "${LOG_LEVEL_DEFAULT}",
    "format": "text",
    "add_source": true
  }
}
EOF
}

# Combined-mode process helpers

x_start_combined() {
  local cfg=$1
  log_info "starting pgrwl in combined mode with ${cfg}"
  /usr/local/bin/pgrwl daemon -c "${cfg}" -m combined \
    > >(tee -a "$LOG_FILE") \
    2> >(tee -a "$LOG_FILE" >&2) &
  COMBINED_PID=$!
}

x_stop_combined() {
  if [[ -n "${COMBINED_PID:-}" ]]; then
    log_info "stopping combined-mode process (PID ${COMBINED_PID})"
    kill -TERM "${COMBINED_PID}" 2>/dev/null || true
    wait "${COMBINED_PID}" 2>/dev/null || true
    COMBINED_PID=""
  fi
}

# REST helpers 

# Wait until the combined-mode HTTP server is accepting requests.
x_wait_combined_ready() {
  local timeout="${1:-30}"
  log_info "waiting for combined-mode HTTP server on ${COMBINED_ADDR}"
  x_wait_http_ok "http://${COMBINED_ADDR}/healthz" "${timeout}"
}

# POST /receiver {"state":"<state>"} and return the HTTP status code.
x_receiver_set_state() {
  local state=$1
  log_info "POST /receiver -> state=${state}"
  curl -fsS -o /dev/null -w "%{http_code}" \
    -X POST "http://${COMBINED_ADDR}/receiver" \
    -H "Content-Type: application/json" \
    -d "{\"state\":\"${state}\"}"
}

# GET /receiver and print the JSON response.
x_receiver_get() {
  curl -fsS "http://${COMBINED_ADDR}/receiver"
}

# Assert that GET /receiver returns the expected .state field.
x_assert_receiver_state() {
  local expected=$1
  local actual
  actual=$(x_receiver_get | grep -o '"state":"[^"]*"' | cut -d'"' -f4)
  if [[ "${actual}" != "${expected}" ]]; then
    log_fatal "expected receiver state '${expected}', got '${actual}'"
  fi
  log_info "receiver state is '${actual}' (ok)"
}

# Main test 

x_test_combined() {
  echo_delim "cleanup state"
  x_remake_dirs
  x_remake_config

  echo_delim "init and start a PostgreSQL cluster"
  xpg_rebuild
  xpg_start
  xpg_recreate_slots

  # Also run pg_receivewal so we can diff WAL archives at the end.
  echo_delim "starting pg_receivewal (reference receiver)"
  x_start_pg_receivewal

  # Phase 1: start combined mode with auto_start: false 

  echo_delim "starting pgrwl in combined mode (receiver initially stopped)"
  x_start_combined "/tmp/config.json"
  x_wait_combined_ready 30

  echo_delim "verify: receiver is stopped after startup (auto_start: false)"
  x_assert_receiver_state "stopped"

  # Phase 2: start receiver via REST API 

  echo_delim "start WAL streaming via POST /receiver"
  http_code=$(x_receiver_set_state "running")
  if [[ "${http_code}" != "200" ]]; then
    log_fatal "expected HTTP 200 when starting receiver, got ${http_code}"
  fi
  x_assert_receiver_state "running"

  # Give the receiver a moment to connect and begin streaming.
  sleep 3

  # Phase 3: idempotent start 

  echo_delim "idempotent start: POST running -> running must return 409"
  http_code=$(x_receiver_set_state "running" || true)
  if [[ "${http_code}" != "409" ]]; then
    log_fatal "expected HTTP 409 on duplicate start, got ${http_code}"
  fi
  x_assert_receiver_state "running"

  # Phase 4: take a base backup while receiver is running 

  echo_delim "creating base backup"
  /usr/local/bin/pgrwl backup -c "/tmp/config.json"

  # Phase 5: generate data, stop receiver via REST, verify HTTP stays up -

  echo_delim "generating WAL segments"
  x_generate_wal 20

  xpg_wait_for_slot "pgrwl_v5"

  echo_delim "stop WAL streaming via POST /receiver (HTTP server must keep running)"
  http_code=$(x_receiver_set_state "stopped")
  if [[ "${http_code}" != "200" ]]; then
    log_fatal "expected HTTP 200 when stopping receiver, got ${http_code}"
  fi
  x_assert_receiver_state "stopped"

  # Healthz must still respond - the process is alive, only streaming is off.
  echo_delim "verify healthz still responds after receiver stop"
  x_wait_http_ok "http://${COMBINED_ADDR}/healthz" 5

  # Phase 6: restart receiver via REST 

  echo_delim "restart WAL streaming via POST /receiver"
  http_code=$(x_receiver_set_state "running")
  if [[ "${http_code}" != "200" ]]; then
    log_fatal "expected HTTP 200 when restarting receiver, got ${http_code}"
  fi
  x_assert_receiver_state "running"

  sleep 3

  # Phase 7: generate more data; save state before disaster 

  echo_delim "running pgbench after restart"
  pgbench -i -s 5 postgres

  # Force a final WAL flush and let both receivers catch up.
  psql -v ON_ERROR_STOP=1 -U postgres -c "checkpoint;" >/dev/null
  psql -v ON_ERROR_STOP=1 -U postgres -c "select pg_switch_wal();" >/dev/null
  sleep 3

  xpg_wait_for_slot "pgrwl_v5"
  xpg_wait_for_slot "pg_receivewal"

  echo_delim "saving pre-disaster pg_dumpall"
  pg_dumpall -f "/tmp/pgdumpall-before" --restrict-key=0

  # Phase 8: simulate disaster 

  echo_delim "teardown (simulate crash)"
  # Stop receiver via REST before stopping the process so the slot is released
  # cleanly - mirrors what an operator would do.
  x_receiver_set_state "stopped" >/dev/null || true
  sleep 1
  x_stop_combined
  x_stop_pg_receivewal
  xpg_teardown

  # Phase 9: restore from backup 

  echo_delim "restoring base backup"
  /usr/local/bin/pgrwl restore --dest="${PGDATA}" -c "/tmp/config.json"
  chmod 0750 "${PGDATA}"
  chown -R postgres:postgres "${PGDATA}"
  touch "${PGDATA}/recovery.signal"

  # Rename *.partial files so PostgreSQL can use them during recovery.
  find "${WAL_PATH}" -type f -name "*.partial" \
    -exec bash -c 'for f; do mv -v "$f" "${f%.partial}"; done' _ {} +
  find "${PG_RECEIVEWAL_WAL_PATH}" -type f -name "*.partial" \
    -exec bash -c 'for f; do mv -v "$f" "${f%.partial}"; done' _ {} +

  # Phase 10: start combined mode for restore_command
  #
  # Key assertion: in combined mode a second 'serve' process is NOT needed.
  # The same process that receives WAL also serves it.  Here we start combined
  # with auto_start: false because we only need the WAL serving endpoint -
  # streaming to a destroyed cluster makes no sense.

  echo_delim "starting combined mode (serving only, receiver stopped) for restore_command"
  xpg_config
  cat <<EOF >>"${PG_CFG}"
restore_command = 'pgrwl restore-command --serve-addr=${COMBINED_ADDR} %f %p'
EOF

  x_start_combined "/tmp/config.json"
  x_wait_combined_ready 30

  x_assert_receiver_state "stopped"

  # Phase 11: start PostgreSQL in recovery 

  >/var/log/postgresql/pg.log
  echo_delim "starting PostgreSQL in recovery"
  xpg_start

  xpg_wait_is_in_recovery
  cat /var/log/postgresql/pg.log

  # Phase 12: compare dumps

  echo_delim "comparing pg_dumpall before vs after"
  pg_dumpall -f "/tmp/pgdumpall-after" --restrict-key=0
  diff "/tmp/pgdumpall-before" "/tmp/pgdumpall-after"

  # Phase 13: compare WAL archives

  echo_delim "comparing pgrwl WAL archive with pg_receivewal archive"
  find "${WAL_PATH}" -type f -name "*.json" -delete
  rm -rf "${WAL_PATH}/backups"
  bash "/var/lib/postgresql/scripts/utils/dircmp.sh" "${WAL_PATH}" "${PG_RECEIVEWAL_WAL_PATH}"

  # Phase 14: start receiver again via REST on the recovered cluster
  # TODO: timeline changed !!!

  echo_delim "verify receiver can be started again on the recovered cluster"
  http_code=$(x_receiver_set_state "running")
  if [[ "${http_code}" != "200" ]]; then
    log_fatal "expected HTTP 200 when starting receiver post-recovery, got ${http_code}"
  fi
  sleep 2
  x_assert_receiver_state "running"

  echo_delim "stop combined-mode process"
  x_stop_combined
}

x_test_combined "${@}"
