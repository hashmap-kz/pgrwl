#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/tests/utils.sh

x_remake_config() {
  cat <<EOF > "${TMPDIR}/config.json"
{
  "main": {
    "listen_port": 7070,
    "directory": "${TMPDIR}/wal-archive"
  },
  "receiver": {
    "slot": "pgrwl_v5",
    "no_loop": true
  },
  "log": {
    "level": "trace",
    "format": "text",
    "add_source": true
  }
}
EOF
}

x_backup_restore() {
  echo_delim "cleanup state"
  x_remake_dirs
  x_remake_config

  # rerun the cluster
  echo_delim "init and run a cluster"
  xpg_rebuild
  xpg_start
  xpg_recreate_slots

  # run wal-receivers
  echo_delim "running wal-receivers"
  # run wal-receiver
  nohup /usr/local/bin/pgrwl start -c "${TMPDIR}/config.json" -m receive >>"$LOG_FILE" 2>&1 &
  # run pg_receivewal
  nohup pg_receivewal \
    -D "${PG_RECEIVEWAL_WAL_PATH}" \
    -S pg_receivewal \
    --no-loop \
    --verbose \
    --no-password \
    --synchronous \
    --dbname "dbname=replication options=-cdatestyle=iso replication=true application_name=pg_receivewal" \
    >>"${PG_RECEIVEWAL_LOG_FILE}" 2>&1 &

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
  for ((i = 0; i < 100; i++)); do
    psql -U postgres -c 'drop table if exists xxx; select pg_switch_wal(); create table if not exists xxx(id serial);'
  done

  # remember the state
  pg_dumpall -f "${TMPDIR}/pgdumpall-before" --restrict-key=0

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
  find "${PG_RECEIVEWAL_WAL_PATH}" -type f -name "*.partial" -exec bash -c 'for f; do mv -v "$f" "${f%.partial}"; done' _ {} +

  # fix configs
  xpg_config
  cat <<EOF >>"${PG_CFG}"
#restore_command = 'cp ${WAL_PATH}/%f %p'
restore_command = 'pgrwl restore-command --serve-addr=127.0.0.1:7070 %f %p'
EOF

  # run serve-mode
  echo_delim "running wal fetcher"
  nohup /usr/local/bin/pgrwl start -c "${TMPDIR}/config.json" -m serve >>"$LOG_FILE" 2>&1 &

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
  pg_dumpall -f "${TMPDIR}/pgdumpall-after" --restrict-key=0
  diff "${TMPDIR}/pgdumpall-before" "${TMPDIR}/pgdumpall-after"

  # compare with pg_receivewal
  echo_delim "compare wal-archive with pg_receivewal"
  find "${WAL_PATH}" -type f -name "*.json" -delete
  bash "/var/lib/postgresql/scripts/utils/dircmp.sh" "${WAL_PATH}" "${PG_RECEIVEWAL_WAL_PATH}"
}

x_backup_restore "${@}"
