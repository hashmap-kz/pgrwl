#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/tests/utils.sh

# Test: REST API endpoints of receive mode
#
# Verifies that all HTTP endpoints respond correctly while the receiver
# process stays alive as a single long-running daemon throughout the test.
# The receiver is started and stopped exclusively via the REST API.
#
# Endpoints covered:
#   GET  /healthz
#   GET  /receiver
#   POST /receiver/states/running   (start, idempotent 409)
#   POST /receiver/states/stopped   (stop, idempotent 200)
#   GET  /status
#   GET  /config
#   GET  /wal/{filename}
#   DELETE /wal-before/{filename}

RECEIVER_ADDR="http://127.0.0.1:7070"

x_remake_config() {
  cat <<EOF > "/tmp/config.json"
{
  "main": {
    "listen_port": 7070,
    "directory": "/tmp/wal-archive"
  },
  "receiver": {
    "slot": "pgrwl_v5",
    "no_loop": true,
    "uploader": {
      "sync_interval": "5s",
      "max_concurrency": 2
    }
  },
  "log": {
    "level": "${LOG_LEVEL_DEFAULT}",
    "format": "text",
    "add_source": true
  },
  "storage": {
    "name": "local"
  }
}
EOF
}

# Assert that the .state field in a ReceiverStateResponse JSON equals expected.
x_assert_receiver_state() {
  local expected=$1
  local actual
  actual=$(curl -fsS "${RECEIVER_ADDR}/receiver" | grep -o '"state":"[^"]*"' | cut -d'"' -f4)
  if [[ "${actual}" != "${expected}" ]]; then
    log_fatal "expected receiver state '${expected}', got '${actual}'"
  fi
  log_info "receiver state is '${actual}' (ok)"
}

# Assert HTTP status code for a given curl invocation.
# Usage: x_assert_http_status <expected_code> <curl args...>
#
# Note: -f is intentionally omitted so that 4xx/5xx responses do not
# cause curl to exit non-zero, which would corrupt the captured output.
x_assert_http_status() {
  local expected=$1
  shift
  local actual
  actual=$(curl -sS -o /dev/null -w "%{http_code}" "$@")
  if [[ "${actual}" != "${expected}" ]]; then
    log_fatal "expected HTTP ${expected}, got ${actual} for: $*"
  fi
  log_info "HTTP ${actual} (ok)"
}

x_test_rest_api() {
  echo_delim "cleanup state"
  x_remake_dirs
  x_remake_config

  echo_delim "init and run a cluster"
  xpg_rebuild
  xpg_start
  xpg_recreate_slots

  # Start the receiver process. It begins streaming immediately.
  echo_delim "starting receiver"
  x_start_receiver "/tmp/config.json"

  # Wait for the HTTP server to be ready before issuing any API calls.
  x_wait_http_ok "${RECEIVER_ADDR}/healthz" 30

  ########################################################################
  ### GET /healthz

  echo_delim "GET /healthz"
  x_assert_http_status "200" "${RECEIVER_ADDR}/healthz"

  ########################################################################
  ### GET /receiver — running state

  echo_delim "GET /receiver (expect: running)"
  x_assert_receiver_state "running"

  ########################################################################
  ### POST /receiver/states/running — idempotent: already running -> 409

  echo_delim "POST /receiver/states/running while already running (expect: 409)"
  x_assert_http_status "409" -X POST "${RECEIVER_ADDR}/receiver/states/running"

  ########################################################################
  ### GET /status — legacy alias

  echo_delim "GET /status"
  x_assert_http_status "200" "${RECEIVER_ADDR}/status"

  ########################################################################
  ### GET /config

  echo_delim "GET /config"
  x_assert_http_status "200" "${RECEIVER_ADDR}/config"
  cfg_body=$(curl -fsS "${RECEIVER_ADDR}/config")
  if ! echo "${cfg_body}" | grep -q '"retention_enable"'; then
    log_fatal "GET /config response missing retention_enable field: ${cfg_body}"
  fi
  log_info "GET /config body ok"

  ########################################################################
  ### Generate WAL and wait for slot to catch up

  echo_delim "generating WAL"
  x_generate_wal 10
  xpg_wait_for_slot "pgrwl_v5"

  ########################################################################
  ### POST /receiver/states/stopped — stop streaming

  echo_delim "POST /receiver/states/stopped (expect: 200, state -> stopped)"
  x_assert_http_status "200" -X POST "${RECEIVER_ADDR}/receiver/states/stopped"
  x_assert_receiver_state "stopped"

  ########################################################################
  ### POST /receiver/states/stopped — idempotent: already stopped -> 200

  echo_delim "POST /receiver/states/stopped while already stopped (expect: 200)"
  x_assert_http_status "200" -X POST "${RECEIVER_ADDR}/receiver/states/stopped"
  x_assert_receiver_state "stopped"

  ########################################################################
  ### GET /healthz — HTTP server stays alive after receiver is stopped

  echo_delim "GET /healthz while receiver is stopped (HTTP server must stay alive)"
  x_assert_http_status "200" "${RECEIVER_ADDR}/healthz"

  ########################################################################
  ### GET /wal/{filename} — serve a WAL file while receiver is stopped

  echo_delim "GET /wal/{filename}"

  # GetWalFile checks: baseDir/filename first, then baseDir/filename.partial.
  # We request the segment by its base name (without .partial); the handler
  # finds and serves the .partial file automatically. This avoids renaming
  # anything in the archive, which would corrupt the LSN resume point and
  # cause the receiver to fail when restarted.
  first_wal=$(ls "${WAL_PATH}"/ | grep -E "^[0-9A-F]{24}\.partial$"     | sed 's/\.partial$//' | sort | head -1 || true)

  # If no partial exists, a completed segment is also acceptable.
  if [[ -z "${first_wal}" ]]; then
    first_wal=$(ls "${WAL_PATH}"/ | grep -E "^[0-9A-F]{24}$" | sort | head -1 || true)
  fi

  if [[ -z "${first_wal}" ]]; then
    log_fatal "no WAL segment found in ${WAL_PATH} (contents: $(ls -la ${WAL_PATH}/))"
  fi
  log_info "fetching WAL file: ${first_wal}"

  curl -fsS "${RECEIVER_ADDR}/wal/${first_wal}" -o /tmp/fetched.wal
  if [[ ! -s /tmp/fetched.wal ]]; then
    log_fatal "GET /wal/${first_wal} returned an empty body"
  fi
  log_info "GET /wal/${first_wal} ok ($(wc -c < /tmp/fetched.wal) bytes)"

  ########################################################################
  ### GET /wal/{filename} — non-existent file -> 404

  echo_delim "GET /wal/nonexistent (expect: 404)"
  x_assert_http_status "404" "${RECEIVER_ADDR}/wal/000000099999999999999999"

  ########################################################################
  ### POST /receiver/states/running — restart after stop

  echo_delim "POST /receiver/states/running (restart after stop, expect: 200)"
  # With no_loop:true the receiver exits after each disconnect and leaves
  # completed segments on disk. findStreamingStart resumes from the segment
  # after the last completed one. If Postgres has not yet flushed that far
  # it rejects the connection with "requested starting point is ahead of
  # flush position". Generate WAL first so Postgres advances past the
  # resume point before the receiver connects.
  x_generate_wal 3
  x_assert_http_status "200" -X POST "${RECEIVER_ADDR}/receiver/states/running"
  xpg_wait_for_slot "pgrwl_v5"
  x_assert_receiver_state "running"

  ########################################################################
  ### DELETE /wal-before/{filename} — schedule WAL deletion

  echo_delim "DELETE /wal-before/${first_wal}"
  x_assert_http_status "200" -X DELETE "${RECEIVER_ADDR}/wal-before/${first_wal}"

  # A second identical request hits the job queue; it may return 200 (if the
  # first job has already finished) or 409 (queue full). Both are acceptable.
  status=$(curl -o /dev/null -w "%{http_code}" -s \
    -X DELETE "${RECEIVER_ADDR}/wal-before/${first_wal}")
  if [[ "${status}" != "200" && "${status}" != "409" ]]; then
    log_fatal "expected 200 or 409 for duplicate DELETE, got ${status}"
  fi
  log_info "duplicate DELETE /wal-before got ${status} (ok)"

  ########################################################################
  ### Final state check

  echo_delim "GET /receiver (final state check, expect: running)"
  x_assert_receiver_state "running"

  echo_delim "done"
}

x_test_rest_api "${@}"
