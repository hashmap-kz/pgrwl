#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/pg/pg.sh

export BASEBACKUP_PATH="/tmp/basebackup"
export WAL_PATH="/tmp/wal-archive"
export WAL_PATH_PG_RECEIVEWAL="/tmp/wal-archive-pg_receivewal"

x_remake_dirs() {
  # cleanup possible state
  rm -rf "${BASEBACKUP_PATH}" && mkdir -p "${BASEBACKUP_PATH}"
  rm -rf "${WAL_PATH}" && mkdir -p "${WAL_PATH}"
  rm -rf "${WAL_PATH_PG_RECEIVEWAL}" && mkdir -p "${WAL_PATH_PG_RECEIVEWAL}"
}

x_backup_restore() {
  x_remake_dirs

  # rerun the cluster
  echo_delim "init and run a cluster"
  xpg_rebuild
  xpg_start

  # run wal-receivers
  echo_delim "running wal-receivers"
  # run wal-receiver
  bash "/var/lib/postgresql/scripts/pg/run_receiver.sh" "start"
  # run pg_receivewal
  bash "/var/lib/postgresql/scripts/pg/run_pg_receivewal.sh" "start"

  # make a basebackup before doing anything
  echo_delim "creating basebackup"
  pg_basebackup \
    --pgdata="${BASEBACKUP_PATH}/data" \
    --wal-method=none \
    --checkpoint=fast \
    --progress \
    --no-password \
    --verbose

  # trying to write ~100 of WAL files as quick as possible
  for ((i=0; i<100; i++)); do
    psql -U postgres -c 'drop table if exists xxx; select pg_switch_wal(); create table if not exists xxx(id serial);'
  done

  # remember the state
  pg_dumpall -f /tmp/pg_dumpall-before

  # stop cluster, cleanup data
  echo_delim "teardown"
  xpg_teardown

  # restore from backup
  echo_delim "restoring backup"
  mv "${BASEBACKUP_PATH}/data" "${PGDATA}"
  chmod 0750 "${PGDATA}"
  chown -R postgres:postgres "${PGDATA}"
  touch "${PGDATA}/recovery.signal"

  # prepare archive (all partial files contain valid wal-segments)
  find "${WAL_PATH}" -type f -name "*.partial" -exec bash -c 'for f; do mv -v "$f" "${f%.partial}"; done' _ {} +
  find "${WAL_PATH_PG_RECEIVEWAL}" -type f -name "*.partial" -exec bash -c 'for f; do mv -v "$f" "${f%.partial}"; done' _ {} +

  # fix configs
  xpg_config
  cat <<EOF >>"${PG_CFG}"
#restore_command = 'cp ${WAL_PATH}/%f %p'
restore_command = 'pgrwl restore-command --serve-addr=127.0.0.1:7070 %f %p'
EOF

  # run serve-mode
  echo_delim "running wal fetcher"
  bash "/var/lib/postgresql/scripts/pg/run_serve_mode.sh" "start"

  # cleanup logs
  >/var/log/postgresql/pg.log

  # run restored cluster
  echo_delim "running cluster"
  xpg_start

  # wait until is in recovery, check logs, etc...
  xpg_wait_is_in_recovery
  cat /var/log/postgresql/pg.log

  # check diffs
  echo_delim "running diff on pg_dumpall dumps (before vs after)"
  pg_dumpall -f /tmp/pg_dumpall-arter
  diff /tmp/pg_dumpall-before /tmp/pg_dumpall-arter

  # compare with pg_receivewal
  echo_delim "compare wal-archive with pg_receivewal"
  bash "/var/lib/postgresql/scripts/utils/dircmp.sh" "${WAL_PATH}/wal_receive" "${WAL_PATH_PG_RECEIVEWAL}"
}

x_backup_restore "${@}"
