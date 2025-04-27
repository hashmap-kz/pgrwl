#!/usr/bin/env bash
# set -euo pipefail

# custom
export PG_MAJOR=17
export PG_BINDIR="/usr/lib/postgresql/${PG_MAJOR}/bin"
export PG_CFG="/etc/postgresql/${PG_MAJOR}/main/postgresql.conf"
export PG_HBA="/etc/postgresql/${PG_MAJOR}/main/pg_hba.conf"
export PG_CONN_STR="postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"

export PATH="${PG_BINDIR}:${PATH}"

# connstr
export PGDATA="/var/lib/postgresql/${PG_MAJOR}/main"
export PGHOST=localhost
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=postgres

export BB_TARGET='/tmp/3c1b7f7c-bfcc-46c4-9d51-5ff163624c5e'

xpg_basebackup() {
  rm -rf "${BB_TARGET}"

  # --no-slot \
  # --wal-method=none \

  pg_basebackup \
    --dbname="${PG_CONN_STR}" \
    --pgdata="${BB_TARGET}" \
    --checkpoint=fast \
    --progress \
    --no-password \
    --verbose
}

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
    echo "Cluster is in recovery, waiting one second..."
    sleep 1
    is_in_recovery=$(psql -At -c "SELECT pg_catalog.pg_is_in_recovery()")
  done
}

xpg_config() {
  cat <<'EOF' >"${PG_HBA}"
local all         all     trust
local replication all     trust
host  all         all all trust
host  replication all all trust
EOF

  cat <<'EOF' >"${PG_CFG}"
listen_addresses    = '*'
logging_collector   = on
log_directory       = '/var/log/postgresql'
log_filename        = 'pg.log'
log_lock_waits      = on
log_temp_files      = 0
log_checkpoints     = on
log_connections     = off
log_destination     = 'stderr'
log_error_verbosity = 'DEFAULT' # TERSE, DEFAULT, VERBOSE
log_hostname        = off
log_min_messages    = 'WARNING' # DEBUG5, DEBUG4, DEBUG3, DEBUG2, DEBUG1, INFO, NOTICE, WARNING, ERROR, LOG, FATAL, PANIC
log_timezone        = 'Asia/Aqtau'
log_line_prefix     = '%t [%p-%l] %r %q%u@%d '
EOF
}
