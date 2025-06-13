#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/tests/utils.sh

x_remake_config() {
  cat <<EOF >/tmp/config.json
{
  "main": {
     "listen_port": 7070,
     "directory": "${WAL_PATH}"
  },
  "receiver": {
     "slot": "pgrwl_v5",
     "no_loop": true,
     "uploader": {
       "sync_interval": "5s",
       "max_concurrency": 4
     }
  },
  "log": {
    "level": "trace",
    "format": "text",
    "add_source": true
  },
  "storage": {
    "name": "s3",
    "compression": {
      "algo": "gzip"
    },
    "encryption": {
      "algo": "aes-256-gcm",
      "pass": "qwerty123"
    },
    "s3": {
      "url": "https://minio:9000",
      "access_key_id": "minioadmin",
      "secret_access_key": "minioadmin123",
      "bucket": "backups",
      "region": "main",
      "use_path_style": true,
      "disable_ssl": true
    }
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

  # start receiving from the fresh WAL that is identical for both receivers
  psql -U postgres -c 'drop table if exists xxx; select pg_switch_wal(); create table if not exists xxx (id serial);'

  # run wal-receivers
  echo_delim "running wal-receivers"
  # run wal-receiver
  nohup /usr/local/bin/pgrwl start -c "/tmp/config.json" -m receive >>"$LOG_FILE" 2>&1 &

  # make a backup before doing anything
  echo_delim "creating backup"
  /usr/local/bin/pgrwl backup -c "/tmp/config.json"

  # run inserts in a background
  bash "/var/lib/postgresql/scripts/pg/run_inserts.sh" start

  # fill with 1M rows
  echo_delim "running pgbench"
  pgbench -i -s 10 postgres

  # wait a little
  sleep 5

  # stop inserts
  pkill -f inserts.sh

  # remember the state
  pg_dumpall -f /tmp/pg_dumpall-before

  # stop cluster, cleanup data
  echo_delim "teardown"
  xpg_teardown

  # restore from backup
  echo_delim "restoring backup"
  /usr/local/bin/pgrwl restore --dest="${PGDATA}" -c "/tmp/config.json"
  chmod 0750 "${PGDATA}"
  chown -R postgres:postgres "${PGDATA}"
  touch "${PGDATA}/recovery.signal"

  # prepare archive (all partial files contain valid wal-segments)
  find "${WAL_PATH}" -type f -name "*.partial" -exec bash -c 'for f; do mv -v "$f" "${f%.partial}"; done' _ {} +

  # fix configs
  xpg_config
  cat <<EOF >>"${PG_CFG}"
restore_command = 'pgrwl restore-command --serve-addr=127.0.0.1:7070 %f %p'
EOF

  # run serve-mode
  echo_delim "running wal fetcher"
  nohup /usr/local/bin/pgrwl start -c "/tmp/config.json" -m serve >>"$LOG_FILE" 2>&1 &

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

  # read the latest rec
  echo_delim "read latest applied records"
  echo "table content:"
  psql --pset pager=off -c "select * from public.tslog;"
  echo "insert log content:"
  tail -10 /tmp/insert-ts.log
}

x_backup_restore "${@}"
