#!/usr/bin/env bash
# set -euo pipefail
. /var/lib/postgresql/scripts/pg/utils.sh

# custom
export PG_MAJOR="${PG_MAJOR:-17}"
export PG_BINDIR="/usr/lib/postgresql/${PG_MAJOR}/bin"
export PG_CFG="/etc/postgresql/${PG_MAJOR}/main/postgresql.conf"
export PG_HBA="/etc/postgresql/${PG_MAJOR}/main/pg_hba.conf"
export PG_CONN_STR="postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"

export PATH="${PG_BINDIR}:${PATH}"

# connstr
export PGDATA="/var/lib/postgresql/${PG_MAJOR}/main/pgdata"
export PGHOST=localhost
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=postgres

# maintain

xpg_dirs() {
    mkdir -p "${PGDATA}"
    chmod 0750 "${PGDATA}"
    chown -R postgres:postgres "/var/lib/postgresql"
    chown -R postgres:postgres "/etc/postgresql"
    
    mkdir -p /var/log/postgresql
    chown -R postgres:postgres /var/log/postgresql
}

xpg_teardown() {
    pkill -9 postgres || true
    rm -rf "${PGDATA}"
}

xpg_start() {
    "${PG_BINDIR}/pg_ctl" \
    -D ${PGDATA} \
    -o "-c config_file=${PG_CFG}" \
    -o "-c hba_file=${PG_HBA}" \
    -l /var/log/postgresql/pg.log \
    start
    xpg_wait_is_ready
}

xpg_rebuild() {
    xpg_teardown
    "${PG_BINDIR}/initdb" "${PGDATA}"
    xpg_config
}

xpg_wait_is_ready() {
    until "${PG_BINDIR}/pg_isready" -d "${PG_CONN_STR}" >/dev/null 2>&1; do
        echo "Waiting for PostgreSQL to be ready..."
        sleep 1
    done
}

xpg_wait_is_in_recovery() {
    is_in_recovery=$(psql -At -c "SELECT pg_catalog.pg_is_in_recovery()")
    until [[ "${is_in_recovery}" == "f" ]]; do
        log_info "Cluster is in recovery, waiting one second..."
        sleep 1
        is_in_recovery=$(psql -At -c "SELECT pg_catalog.pg_is_in_recovery()")
    done
}

xpg_recreate_slots() {
"${PG_BINDIR}/psql" -v ON_ERROR_STOP=1 <<'EOSQL'
  -- pgrwl
  SELECT pg_drop_replication_slot('pgrwl_v5') WHERE EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = 'pgrwl_v5');
  SELECT pg_switch_wal();
  SELECT * FROM pg_create_physical_replication_slot('pgrwl_v5', true, false);
  -- pg_receivewal
  SELECT pg_drop_replication_slot('pg_receivewal') WHERE EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = 'pg_receivewal');
  SELECT pg_switch_wal();
  SELECT * FROM pg_create_physical_replication_slot('pg_receivewal', true, false);
  -- starting point
  CHECKPOINT;
  SELECT pg_switch_wal();
EOSQL
    
}

xpg_create_slots() {
"${PG_BINDIR}/psql" -v ON_ERROR_STOP=1 <<'EOSQL'
  -- pgrwl
  SELECT * FROM pg_create_physical_replication_slot('pgrwl_v5', true, false);
  -- pg_receivewal
  SELECT * FROM pg_create_physical_replication_slot('pg_receivewal', true, false);
  -- starting point
  CHECKPOINT;
  SELECT pg_switch_wal();
EOSQL
    
}

xpg_checkpoint_switch_wal() {
"${PG_BINDIR}/psql" -v ON_ERROR_STOP=1 <<'EOSQL'
  -- starting point
  CHECKPOINT;
  SELECT pg_switch_wal();
EOSQL
    
}

xpg_wait_for_slot() {
    local slot="$1"
    
    # target LSN = current WAL write position
    local target_lsn
    target_lsn=$(psql -At -U postgres -c "SELECT pg_current_wal_lsn()")
    
    echo_delim "waiting for slot ${slot} to reach ${target_lsn}"
    
    # tiny polling loop; tune timeout (if needed)
    for i in {1..120}; do
        local confirmed
        confirmed=$(psql -At -U postgres -c "SELECT restart_lsn FROM pg_replication_slots WHERE slot_name = '${slot}'")
        
        # slot might not exist yet -> treat as 0/0
        if [[ -z "$confirmed" ]]; then
            confirmed="0/0"
        fi
        
        local ok
        ok=$(psql -At -U postgres -c \
        "SELECT '${confirmed}' >= '${target_lsn}'")
        
        if [[ "$ok" = "t" ]]; then
            echo "slot ${slot} caught up at ${confirmed}"
            return 0
        fi
        
        sleep 0.25
    done
    
    echo "slot ${slot} failed to catch up in time"
    return 1
}

xpg_config() {
  cat <<'EOF' >"${PG_HBA}"
local all         all     trust
local replication all     trust
host  all         all all trust
host  replication all all trust
EOF
    
  cat <<'EOF' >"${PG_CFG}"
listen_addresses         = '*'
logging_collector        = on
log_directory            = '/var/log/postgresql'
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
EOF
}
