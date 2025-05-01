#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/pg/pg.sh

export BASEBACKUP_PATH="/tmp/basebackup"
export WAL_PATH="/tmp/wal-archive"

x_backup_restore() {
  # rerun the cluster
  xpg_rebuild
  xpg_start

  # run wal-receiver
  bash "/var/lib/postgresql/scripts/pg/run_receiver.sh" "start"

  # make a basebackup before doing anything
  rm -rf "${BASEBACKUP_PATH}"
  mkdir -p "${BASEBACKUP_PATH}"
  pg_basebackup \
    --pgdata="${BASEBACKUP_PATH}/data" \
    --wal-method=none \
    --checkpoint=fast \
    --progress \
    --no-password \
    --verbose

  # run fresh cluster, make basebackup, fill with 1M rows
  pgbench -i -s 10 postgres

  # remember the state
  pg_dumpall -f /tmp/pg_dumpall-before

  # stop cluster, cleanup data
  xpg_teardown

  # restore from backup
  mv "${BASEBACKUP_PATH}/data" "${PGDATA}"
  chmod 0750 "${PGDATA}"
  chown -R postgres:postgres "${PGDATA}"
  touch "${PGDATA}/recovery.signal"

  # prepare archive (all partial files contain valid wal-segments)
  find "${WAL_PATH}" -type f -name "*.partial" -exec bash -c 'for f; do mv -v "$f" "${f%.partial}"; done' _ {} +

  # fix configs
  xpg_config
  cat <<EOF >>"${PG_CFG}"
restore_command = 'cp ${WAL_PATH}/%f %p'
EOF

  # cleanup logs
  >/var/log/postgresql/pg.log

  # run restored cluster
  xpg_start

  # wait until is in recovery, check logs, etc...
  xpg_wait_is_in_recovery
  cat /var/log/postgresql/pg.log

  # check diffs
  pg_dumpall -f /tmp/pg_dumpall-arter
  diff /tmp/pg_dumpall-before /tmp/pg_dumpall-arter
}
