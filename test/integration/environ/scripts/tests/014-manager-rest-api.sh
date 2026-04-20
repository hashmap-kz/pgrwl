#!/usr/bin/env bash
set -Eeuo pipefail
. /var/lib/postgresql/scripts/tests/utils.sh

# Manager / REST API integration test.
#
# This test focuses on the HTTP management layer, not on PITR correctness.
# It verifies that:
#
#   * the daemon starts in receive mode by default;
#   * /mode is always reachable;
#   * invalid and duplicate mode transitions return useful HTTP errors;
#   * receive-only endpoints are available only in receive mode;
#   * serve-only endpoints are available only in serve mode;
#   * mode switching swaps the REST handler without restarting the HTTP server;
#   * switching receive -> serve -> receive leaves the manager in a usable state;
#   * DELETE /wal-before/{filename} schedules cleanup through the receive API.

: "${POLL_INTERVAL_SEC:=0.25}"
: "${STREAMING_TIMEOUT_SEC:=30}"

PGRWL_PORT="7070"
PGRWL_ADDR="http://127.0.0.1:${PGRWL_PORT}"
PGRWL_CONFIG="/tmp/manager-rest-api-config.json"
STORAGE_WAL_DIR="${WAL_PATH}/wal-archive"

assert_eq() {
  local want="$1"
  local got="$2"
  local msg="$3"

  if [[ "${got}" != "${want}" ]]; then
    echo "ASSERTION FAILED: ${msg}" >&2
    echo "  want: ${want}" >&2
    echo "  got:  ${got}" >&2
    exit 1
  fi
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local msg="$3"

  if [[ "${haystack}" != *"${needle}"* ]]; then
    echo "ASSERTION FAILED: ${msg}" >&2
    echo "  expected substring: ${needle}" >&2
    echo "  body: ${haystack}" >&2
    exit 1
  fi
}

http_status() {
  local method="$1"
  local path="$2"

  curl -sS -o /tmp/http-body -w '%{http_code}' -X "${method}" "${PGRWL_ADDR}${path}"
}

http_body() {
  cat /tmp/http-body
}

wait_http_status() {
  local method="$1"
  local path="$2"
  local want="$3"
  local timeout="${4:-30}"
  local status

  for _ in $(seq 1 "${timeout}"); do
    status="$(http_status "${method}" "${path}" || true)"
    if [[ "${status}" == "${want}" ]]; then
      return 0
    fi
    sleep 1
  done

  echo "timed out waiting for ${method} ${path} to return ${want}; last status=${status}" >&2
  echo "last body:" >&2
  http_body >&2 || true
  return 1
}

wait_body_contains() {
  local method="$1"
  local path="$2"
  local needle="$3"
  local timeout="${4:-30}"
  local status body

  for _ in $(seq 1 "${timeout}"); do
    status="$(http_status "${method}" "${path}" || true)"
    body="$(http_body || true)"
    if [[ "${status}" == "200" && "${body}" == *"${needle}"* ]]; then
      return 0
    fi
    sleep 1
  done

  echo "timed out waiting for ${method} ${path} body to contain ${needle}" >&2
  echo "last status=${status}" >&2
  echo "last body=${body}" >&2
  return 1
}

stop_pgrwl() {
  if [[ -n "${RECEIVER_PID:-}" ]]; then
    log_info "stopping pgrwl daemon (PID ${RECEIVER_PID})"
    kill -TERM "${RECEIVER_PID}" 2>/dev/null || true
    wait "${RECEIVER_PID}" 2>/dev/null || true
    RECEIVER_PID=""
  fi
}

cleanup_manager_rest_api() {
  set +e
  stop_pgrwl
}
trap cleanup_manager_rest_api EXIT

write_config() {
  cat >"${PGRWL_CONFIG}" <<EOFJSON
{
  "main": {
    "listen_port": ${PGRWL_PORT},
    "directory": "${WAL_PATH}"
  },
  "receiver": {
    "slot": "pgrwl_v5",
    "no_loop": true,
    "retention": {
      "enable": true,
      "sync_interval": "24h",
      "keep_period": "720h"
    }
  },
  "storage": {
    "name": "local"
  },
  "log": {
    "level": "debug",
    "format": "pretty",
    "add_source": false
  }
}
EOFJSON
}

write_test_wal_file() {
  local filename="$1"
  local content="$2"

  printf '%s' "${content}" >"${WAL_PATH}/${filename}"
}

write_storage_wal_file() {
  local filename="$1"
  local content="$2"

  mkdir -p "${STORAGE_WAL_DIR}"
  printf '%s' "${content}" >"${STORAGE_WAL_DIR}/${filename}"
}

wait_file_deleted() {
  local path="$1"
  local timeout="${2:-30}"

  for _ in $(seq 1 "${timeout}"); do
    if [[ ! -e "${path}" ]]; then
      return 0
    fi
    sleep 1
  done

  echo "timed out waiting for file to be deleted: ${path}" >&2
  find "${STORAGE_WAL_DIR}" -maxdepth 1 -type f -printf '%f\n' 2>/dev/null | sort >&2 || true
  return 1
}

echo_delim "prepare PostgreSQL and pgrwl config"
x_remake_dirs
xpg_rebuild
xpg_start
xpg_recreate_slots
write_config

# Files used by the serve-mode route check.
SERVED_WAL="0000000100000000000000AA"
SERVED_BODY="hello-from-serve-mode"
write_test_wal_file "${SERVED_WAL}" "${SERVED_BODY}"

# Files used by DELETE /wal-before. The receive REST service deletes from the
# configured storage backend. For local storage, that backend is rooted at
# ${main.directory}/wal-archive.
DELETE_OLD_A="000000010000000000000001"
DELETE_OLD_B="000000010000000000000002.gz"
DELETE_CUTOFF="000000010000000000000003"
DELETE_AFTER="000000010000000000000004"
write_storage_wal_file "${DELETE_OLD_A}" "old-a"
write_storage_wal_file "${DELETE_OLD_B}" "old-b"
write_storage_wal_file "${DELETE_CUTOFF}" "cutoff"
write_storage_wal_file "${DELETE_AFTER}" "after"

echo_delim "start pgrwl daemon"
x_start_receiver "${PGRWL_CONFIG}"
wait_http_status GET /healthz 200 30
wait_body_contains GET /mode '"mode":"receive"' 30

# Give the WAL receiver a little time to appear in pg_stat_replication. This is
# not the main assertion of the test, but it catches a broken receive supervisor
# early and makes /status checks less flaky.
xpg_wait_until_streaming "pgrwl_v5" 30

echo_delim "verify receive-mode REST API"
status="$(http_status GET /status)"
body="$(http_body)"
assert_eq "200" "${status}" "GET /status should be available in receive mode"
assert_contains "${body}" '"stream_status"' "GET /status should expose stream status"
assert_contains "${body}" '"slot":"pgrwl_v5"' "GET /status should expose the configured slot"
assert_contains "${body}" '"running":true' "GET /status should report a running receiver"

status="$(http_status GET /config)"
body="$(http_body)"
assert_eq "200" "${status}" "GET /config should be available in receive mode"
assert_contains "${body}" '"retention_enable":true' "GET /config should expose retention status"

status="$(http_status GET "/wal/${SERVED_WAL}")"
assert_eq "404" "${status}" "GET /wal/{filename} must not be routed in receive mode"

status="$(http_status POST /mode/nope)"
body="$(http_body)"
assert_eq "400" "${status}" "POST /mode/nope should reject invalid modes"
assert_contains "${body}" 'valid modes: receive, serve' "invalid mode response should describe valid values"

status="$(http_status POST /mode/receive)"
body="$(http_body)"
assert_eq "409" "${status}" "POST /mode/receive should reject duplicate receive transition"
assert_contains "${body}" 'already in receive mode' "duplicate receive transition should explain the conflict"

echo_delim "verify DELETE /wal-before schedules local-storage cleanup"
status="$(http_status DELETE "/wal-before/${DELETE_CUTOFF}")"
body="$(http_body)"
assert_eq "200" "${status}" "DELETE /wal-before/{filename} should schedule cleanup"
assert_contains "${body}" '"status":"scheduled"' "DELETE /wal-before response should confirm scheduling"
wait_file_deleted "${STORAGE_WAL_DIR}/${DELETE_OLD_A}" 30
wait_file_deleted "${STORAGE_WAL_DIR}/${DELETE_OLD_B}" 30

if [[ ! -e "${STORAGE_WAL_DIR}/${DELETE_CUTOFF}" ]]; then
  echo "ASSERTION FAILED: cutoff WAL must not be deleted" >&2
  exit 1
fi
if [[ ! -e "${STORAGE_WAL_DIR}/${DELETE_AFTER}" ]]; then
  echo "ASSERTION FAILED: WAL after cutoff must not be deleted" >&2
  exit 1
fi

echo_delim "switch receive -> serve"
status="$(http_status POST /mode/serve)"
body="$(http_body)"
assert_eq "200" "${status}" "POST /mode/serve should switch to serve mode"
assert_contains "${body}" '"mode":"serve"' "POST /mode/serve response should expose new mode"
wait_body_contains GET /mode '"mode":"serve"' 10

status="$(http_status GET /status)"
assert_eq "404" "${status}" "GET /status must not be routed in serve mode"

status="$(http_status GET "/wal/${SERVED_WAL}")"
body="$(http_body)"
assert_eq "200" "${status}" "GET /wal/{filename} should be available in serve mode"
assert_eq "${SERVED_BODY}" "${body}" "serve mode should return the local WAL file content"

status="$(http_status POST /mode/serve)"
body="$(http_body)"
assert_eq "409" "${status}" "POST /mode/serve should reject duplicate serve transition"
assert_contains "${body}" 'already in serve mode' "duplicate serve transition should explain the conflict"

echo_delim "switch serve -> receive"
status="$(http_status POST /mode/receive)"
body="$(http_body)"
assert_eq "200" "${status}" "POST /mode/receive should switch back to receive mode"
assert_contains "${body}" '"mode":"receive"' "POST /mode/receive response should expose new mode"
wait_body_contains GET /mode '"mode":"receive"' 10
xpg_wait_until_streaming "pgrwl_v5" 30

status="$(http_status GET /status)"
body="$(http_body)"
assert_eq "200" "${status}" "GET /status should be available after switching back to receive"
assert_contains "${body}" '"running":true' "receiver should run after switching back to receive"

status="$(http_status GET "/wal/${SERVED_WAL}")"
assert_eq "404" "${status}" "GET /wal/{filename} must disappear after switching back to receive mode"

echo_delim "manager/rest api test passed"
