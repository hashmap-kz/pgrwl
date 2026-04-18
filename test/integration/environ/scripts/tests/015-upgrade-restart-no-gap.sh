#!/usr/bin/env bash
set -Eeuo pipefail
. /var/lib/postgresql/scripts/tests/utils.sh

# Test: receiver restart/upgrade while writes continue
#
# Why this matters:
#   The most realistic "upgrade" path for pgrwl is: stop the old process,
#   start a new one, and rely on the physical replication slot plus local WAL
#   state to continue streaming without data loss.
#
# What this test validates:
#   1. A base backup is taken before the restart window.
#   2. Continuous writes happen before, during, and after receiver restarts.
#   3. The receiver is restarted multiple times (graceful + hard kill style).
#   4. PostgreSQL is later restored from the base backup + archived WAL.
#   5. The restored logical state exactly matches the original logical state.
#
# In practice, this is the closest end-to-end proof that an in-place upgrade
# or pod replacement does not create WAL gaps visible at recovery time.

RECEIVER_ADDR="http://127.0.0.1:7070"
RESTARTS="${RESTARTS:-3}"
WRITE_BATCHES="${WRITE_BATCHES:-120}"
ROWS_PER_BATCH="${ROWS_PER_BATCH:-50}"
SLEEP_BETWEEN_BATCHES="${SLEEP_BETWEEN_BATCHES:-0.10}"

x_remake_config() {
  cat <<EOF_CFG > "/tmp/config.json"
{
  "main": {
    "listen_port": 7070,
    "directory": "${WAL_PATH}"
  },
  "receiver": {
    "slot": "pgrwl_v5",
    "no_loop": true
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
EOF_CFG
}

x_wait_receiver_http() {
  x_wait_http_ok "${RECEIVER_ADDR}/healthz" 30 || {
    log_fatal "receiver HTTP server did not become ready"
  }
}

x_restart_receiver_graceful() {
  log_info "graceful restart: stop streaming via API, then replace process"

  curl -fsS -X POST "${RECEIVER_ADDR}/receiver/states/stopped" >/dev/null || true
  sleep 1
  x_kill_receiver
  x_start_receiver "/tmp/config.json"
  x_wait_receiver_http
}

x_restart_receiver_hard() {
  log_info "hard restart: kill process and start it again"
  x_kill_receiver
  sleep 1
  x_start_receiver "/tmp/config.json"
  x_wait_receiver_http
}

x_prepare_workload() {
  psql -v ON_ERROR_STOP=1 <<'SQL'
DROP TABLE IF EXISTS public.upgrade_log;
CREATE TABLE public.upgrade_log (
  id           bigserial PRIMARY KEY,
  batch_no     integer      NOT NULL,
  item_no      integer      NOT NULL,
  marker       text         NOT NULL,
  created_at   timestamptz  NOT NULL DEFAULT now(),
  UNIQUE (batch_no, item_no)
);
SQL
}

x_writer_loop() {
  local batches="${1:?batches required}"
  local rows_per_batch="${2:?rows_per_batch required}"
  local sleep_between="${3:?sleep_between required}"
  local i

  for ((i = 1; i <= batches; i++)); do
    psql -v ON_ERROR_STOP=1 <<SQL
INSERT INTO public.upgrade_log(batch_no, item_no, marker)
SELECT
  ${i},
  gs,
  md5(random()::text || clock_timestamp()::text || gs::text || ${i}::text)
FROM generate_series(1, ${rows_per_batch}) AS gs;
SELECT pg_switch_wal();
SQL
    sleep "${sleep_between}"
  done
}

x_collect_expected_state() {
  pg_dumpall -f "/tmp/pgdumpall-before" --restrict-key=0
  grep -vE '^SELECT pg_catalog\.setval\(' /tmp/pgdumpall-before > /tmp/pgdumpall-before.normalized
  psql -X -A -t -q -c "select count(*) from public.upgrade_log" > /tmp/expected_row_count
  psql -X -A -F '|' -t -q -c "select min(id), max(id), count(*), count(distinct id) from public.upgrade_log" > /tmp/expected_id_stats
  psql -X -A -t -q -c "select md5(string_agg(id::text || '|' || batch_no::text || '|' || item_no::text || '|' || marker, ',' order by id)) from public.upgrade_log" > /tmp/expected_table_hash
}

x_collect_restored_state() {
  pg_dumpall -f "/tmp/pgdumpall-after" --restrict-key=0
  grep -vE '^SELECT pg_catalog\.setval\(' /tmp/pgdumpall-after > /tmp/pgdumpall-after.normalized
  psql -X -A -t -q -c "select count(*) from public.upgrade_log" > /tmp/restored_row_count
  psql -X -A -F '|' -t -q -c "select min(id), max(id), count(*), count(distinct id) from public.upgrade_log" > /tmp/restored_id_stats
  psql -X -A -t -q -c "select md5(string_agg(id::text || '|' || batch_no::text || '|' || item_no::text || '|' || marker, ',' order by id)) from public.upgrade_log" > /tmp/restored_table_hash
  psql -X -A -F '|' -t -q -c "select (select last_value from public.upgrade_log_id_seq), (select max(id) from public.upgrade_log)" > /tmp/restored_sequence_state
}

x_assert_same_state() {
  diff "/tmp/pgdumpall-before.normalized" "/tmp/pgdumpall-after.normalized"
  diff "/tmp/expected_row_count" "/tmp/restored_row_count"
  diff "/tmp/expected_id_stats" "/tmp/restored_id_stats"
  diff "/tmp/expected_table_hash" "/tmp/restored_table_hash"

  local stats seq_state min_id max_id cnt distinct_cnt seq_last seq_max_id
  stats="$(cat /tmp/restored_id_stats)"
  IFS='|' read -r min_id max_id cnt distinct_cnt <<<"${stats}"
  if [[ "${cnt}" != "${distinct_cnt}" ]]; then
    log_fatal "restored id stats are inconsistent: ${stats}"
  fi

  seq_state="$(cat /tmp/restored_sequence_state)"
  IFS='|' read -r seq_last seq_max_id <<<"${seq_state}"
  if (( seq_last < seq_max_id )); then
    log_fatal "restored sequence last_value is behind table max(id): ${seq_state}"
  fi

  log_info "restored state matches expected state (rows=${cnt}, ids=${min_id}..${max_id}, seq_last=${seq_last})"
}

x_upgrade_restart_no_gap() {
  local writer_pid=""
  local i

  echo_delim "cleanup state"
  x_remake_dirs
  x_remake_config

  echo_delim "init and run a cluster"
  xpg_rebuild
  xpg_start
  xpg_recreate_slots
  x_prepare_workload

  echo_delim "start receiver"
  x_start_receiver "/tmp/config.json"
  x_wait_receiver_http

  echo_delim "create base backup"
  /usr/local/bin/pgrwl backup -c "/tmp/config.json"

  echo_delim "start continuous writes"
  x_writer_loop "${WRITE_BATCHES}" "${ROWS_PER_BATCH}" "${SLEEP_BETWEEN_BATCHES}" >>/tmp/upgrade-writer.log 2>&1 &
  writer_pid=$!

  echo_delim "restart receiver repeatedly while writes continue"
  for ((i = 1; i <= RESTARTS; i++)); do
    sleep 2

    if (( i % 2 == 1 )); then
      x_restart_receiver_graceful
    else
      x_restart_receiver_hard
    fi

    # Ensure Postgres has advanced WAL after each restart so resume logic is
    # exercised against fresh segments instead of only the already-open one.
    x_generate_wal 3
  done

  echo_delim "wait for writer to finish"
  wait "${writer_pid}"

  echo_delim "force checkpoint and WAL switch"
  psql -v ON_ERROR_STOP=1 -c 'CHECKPOINT;' >/dev/null
  psql -v ON_ERROR_STOP=1 -c 'SELECT pg_switch_wal();' >/dev/null

  echo_delim "wait for replication slot catch-up"
  xpg_wait_for_slot "pgrwl_v5"

  echo_delim "remember expected final state"
  x_collect_expected_state

  echo_delim "teardown source side"
  x_stop_receiver
  xpg_teardown

  echo_delim "restore from base backup"
  /usr/local/bin/pgrwl restore --dest="${PGDATA}" -c "/tmp/config.json"
  chmod 0750 "${PGDATA}"
  chown -R postgres:postgres "${PGDATA}"
  touch "${PGDATA}/recovery.signal"

  echo_delim "prepare WAL archive for restore"
  find "${WAL_PATH}" -type f -name "*.partial" -exec bash -c 'for f; do mv -v "$f" "${f%.partial}"; done' _ {} +

  xpg_config
  cat <<EOF_CFG >>"${PG_CFG}"
restore_command = 'pgrwl restore-command --serve-addr=127.0.0.1:7070 %f %p'
EOF_CFG

  echo_delim "start WAL serving"
  x_start_serving "/tmp/config.json"
  x_wait_receiver_http

  > /var/log/postgresql/pg.log

  echo_delim "start restored cluster"
  xpg_start
  xpg_wait_is_in_recovery

  echo_delim "collect restored state"
  x_collect_restored_state

  echo_delim "verify restored state exactly matches"
  x_assert_same_state

  echo_delim "done"
}

x_upgrade_restart_no_gap "$@"
