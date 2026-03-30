#!/usr/bin/env bash
set -Eeuo pipefail

WORKDIR=/tmp/pgrwl-basic
PGDATA="${WORKDIR}/pgdata"
WAL_ARCHIVE_DIR="${WORKDIR}/wal-archive"
PGRWL_CONFIG="${WORKDIR}/pgrwl-config.json"

DBNAME=bench
REPL_SLOT="pgrwl_v5"

export PGHOST="localhost"
export PGPORT="5432"
export PGUSER="postgres"
export PGPASSWORD="postgres"

PGRWL_PID=""

log() {
  printf '\n[%s] %s\n' "$(date '+%F %T')" "$*"
}

die() {
  echo "ERROR: $*" >&2
  exit 1
}

wait_for_postgres() {
  log "waiting for PostgreSQL to become ready"
  for _ in $(seq 1 120); do
    if pg_isready -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  die "PostgreSQL did not become ready in time"
}

wait_until_out_of_recovery() {
  log "waiting for PostgreSQL to finish recovery"
  for _ in $(seq 1 120); do
    if psql -d postgres -Atqc "select case when pg_is_in_recovery() then 'yes' else 'no' end" 2>/dev/null | grep -q '^no$'; then
      return 0
    fi
    sleep 1
  done
  die "PostgreSQL did not finish recovery in time"
}

stop_postgres() {
  if [[ -d "$PGDATA" ]]; then
    log "stopping PostgreSQL"
    pg_ctl -D "$PGDATA" -m immediate stop >/dev/null 2>&1 || true
  fi
}

stop_pgrwl() {
  if [[ -n "${PGRWL_PID:-}" ]]; then
    log "stopping pgrwl"
    kill "$PGRWL_PID" >/dev/null 2>&1 || true
    wait "$PGRWL_PID" >/dev/null 2>&1 || true
    PGRWL_PID=""
  fi
}

cleanup() {
  stop_pgrwl
  stop_postgres
}
trap cleanup EXIT

### running

sudo pkill -9 postgres || true
sudo pkill -9 pgrwl || true
sudo rm -rf /tmp/*

log "preparing workdir: $WORKDIR"
rm -rf "$WORKDIR"
mkdir -p "$WORKDIR" "$WAL_ARCHIVE_DIR"

log "initializing PostgreSQL cluster"
initdb -D "$PGDATA" -A trust --auth-local=trust --auth-host=trust >/dev/null

cat >>"$PGDATA/postgresql.conf" <<EOF
listen_addresses         = '*'
logging_collector        = on
log_directory            = '${WORKDIR}'
log_filename             = 'pg.log'
log_lock_waits           = on
log_temp_files           = 0
log_checkpoints          = on
log_connections          = off
log_destination          = 'stderr'
log_error_verbosity      = 'DEFAULT' # TERSE, DEFAULT, VERBOSE
log_hostname             = off
log_min_messages         = 'WARNING' # DEBUG5, DEBUG4, DEBUG3, DEBUG2, DEBUG1, INFO, NOTICE, WARNING, ERROR, LOG, FATAL, PANIC
log_timezone             = 'Asia/Aqtau'
log_line_prefix          = '%t [%p-%l] %r %q%u@%d '
wal_level                = replica
max_wal_senders          = 10
wal_keep_size            = 64MB
log_replication_commands = on

max_replication_slots    = 10
shared_buffers           = 256MB
fsync                    = on
synchronous_commit       = on
full_page_writes         = on
EOF

log "starting PostgreSQL"
pg_ctl -D "$PGDATA" -l "$WORKDIR/pg.log" start >/dev/null
wait_for_postgres

log "creating replication slot: $REPL_SLOT"
psql -d postgres -v ON_ERROR_STOP=1 -c "select pg_create_physical_replication_slot('$REPL_SLOT');" >/dev/null

log "creating test database: $DBNAME"
createdb "$DBNAME"

log "writing pgrwl config"
cat >"$PGRWL_CONFIG" <<EOF
{
  "main": {
    "listen_port": 7070,
    "directory": "$WAL_ARCHIVE_DIR"
  },
  "receiver": {
    "slot": "$REPL_SLOT",
    "no_loop": true
  },
  "log": {
    "level": "debug",
    "format": "text",
    "add_source": false
  }
}
EOF

log "starting pgrwl receiver"
pgrwl daemon -m receive -c "$PGRWL_CONFIG" >"$WORKDIR/pgrwl-receive.log" 2>&1 &
PGRWL_PID=$!

sleep 3

log "creating base backup"
pgrwl backup -c "$PGRWL_CONFIG"

log "initializing pgbench with scale=10 (~1 million rows in pgbench_accounts)"
pgbench -i -s 10 "$DBNAME"

log "running a small pgbench workload"
pgbench -c 4 -j 2 -t 200 "$DBNAME"

log "dumping cluster state before destruction"
pg_dumpall --quote-all-identifiers --restrict-key=0 >"$WORKDIR/before.sql"

log "forcing WAL switch and checkpoint before crash simulation"
psql -d postgres -v ON_ERROR_STOP=1 -c "checkpoint;" >/dev/null
psql -d postgres -v ON_ERROR_STOP=1 -c "select pg_switch_wal();" >/dev/null

sleep 3

log "terminating PostgreSQL and pgrwl"
stop_postgres
stop_pgrwl

log "removing original PGDATA"
rm -rf "${PGDATA}"

log "restoring PGDATA from base backup"
pgrwl restore --dest="${PGDATA}" -c "$PGRWL_CONFIG"
chmod 0750 "${PGDATA}"
chown -R postgres:postgres "${PGDATA}"
touch "$PGDATA/recovery.signal"

pgrwl daemon -m serve -c "$PGRWL_CONFIG" >"$WORKDIR/pgrwl-serve.log" 2>&1 &

cat >>"$PGDATA/postgresql.conf" <<EOF
restore_command = 'pgrwl restore-command --serve-addr=127.0.0.1:7070 %f %p'
EOF

log "starting restored PostgreSQL cluster"
pg_ctl -D "$PGDATA" -l "$WORKDIR/postgres-restored.log" start >/dev/null
wait_for_postgres
wait_until_out_of_recovery

log "dumping cluster state after recovery"
pg_dumpall --quote-all-identifiers --restrict-key=0 >"$WORKDIR/after.sql"

log "comparing dumps"
if diff -u "$WORKDIR/before.sql" "$WORKDIR/after.sql" >"$WORKDIR/dump.diff"; then
  log "SUCCESS: dumps are identical"
  echo "before: $WORKDIR/before.sql"
  echo "after : $WORKDIR/after.sql"
  echo "diff  : $WORKDIR/dump.diff (empty)"
else
  echo
  echo "FAIL: dumps differ"
  echo "See: $WORKDIR/dump.diff"
  exit 1
fi
