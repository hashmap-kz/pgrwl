#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/pg/pg.sh

x_backup_restore() {
  local config_path="${1:?Expect argument config_path}"

  # rerun the cluster
  xpg_rebuild
  cat <<EOF >>"${PG_CFG}"
archive_mode = on
archive_command = '/usr/local/bin/pgwal wal-archive -c ${config_path} %p %f'
EOF
  xpg_start

  # run fresh cluster, make basebackup, fill with 1M rows
  /usr/local/bin/pgwal backup -c ${config_path}
  pgbench -i -s 10 postgres
  psql -c 'select pg_walfile_name(pg_current_wal_insert_lsn());'
  psql -c 'CHECKPOINT;'
  psql -c 'select pg_switch_wal();'

  pg_dumpall -f /tmp/pg_dumpall-before

  # stop cluster, cleanup data
  xpg_teardown

  # restore from backup
  /usr/local/bin/pgwal restore -c ${config_path} --dest="${PGDATA}"
  chmod 0750 "${PGDATA}"
  chown -R postgres:postgres "${PGDATA}"
  touch "${PGDATA}/recovery.signal"

  # fix configs
  xpg_config
  cat <<EOF >>"${PG_CFG}"
archive_mode = on
restore_command = '/usr/local/bin/pgwal wal-restore -c ${config_path} %f %p'
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
