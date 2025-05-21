#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/pg/pg.sh

export PG_BASEBACKUP_PATH="/tmp/basebackup"
export WAL_PATH="/mnt/wal-archive"

x_backup_restore() {
  # restore from backup
  echo_delim "restoring backup"
  mv "${PG_BASEBACKUP_PATH}/data" "${PGDATA}"
  chmod 0750 "${PGDATA}"
  chown -R postgres:postgres "${PGDATA}"
  touch "${PGDATA}/recovery.signal"

  # prepare archive (all partial files contain valid wal-segments)
  echo_delim "rename partial files"
  find "${WAL_PATH}" -type f -name "*.partial" -exec bash -c 'for f; do mv -v "$f" "${f%.partial}"; done' _ {} +

  # fix configs
  xpg_config
  cat <<EOF >>"${PG_CFG}"
restore_command = 'cp ${WAL_PATH}/wal_receive/%f %p'
EOF

  # cleanup logs
  >/var/log/postgresql/pg.log

  # run restored cluster
  echo_delim "running cluster"
  xpg_start

  # wait until is in recovery, check logs, etc...
  echo_delim "cluster logs"
  xpg_wait_is_in_recovery
  cat /var/log/postgresql/pg.log
}

x_backup_restore "${@}"
