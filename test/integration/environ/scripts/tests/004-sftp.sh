#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/pg/pg.sh

export BASEBACKUP_PATH="/tmp/basebackup"
export WAL_PATH="/tmp/wal-archive"
export LOG_FILE="/tmp/pgrwl.log"

# Default environment
export PGHOST="localhost"
export PGPORT="5432"
export PGUSER="postgres"
export PGPASSWORD="postgres"

x_remake_dirs() {
  # cleanup possible state
  rm -rf "${BASEBACKUP_PATH}" && mkdir -p "${BASEBACKUP_PATH}"
  rm -rf "${WAL_PATH}" && mkdir -p "${WAL_PATH}"

  cat <<EOF >/tmp/config.json
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
    "level": "trace",
    "format": "text",
    "add_source": true
  },
  "storage": {
    "name": "sftp",
    "uploader": {
      "sync_interval": "5s",
      "max_concurrency": 4
    },
    "compression": {
      "algo": "gzip"
    },
    "encryption": {
      "algo": "aes-256-gcm",
      "pass": "qwerty123"
    },
    "sftp": {
      "host": "vm",
      "port": 22,
      "base_dir": "/home/testuser",
      "user": "testuser",
      "pkey_path": "/var/lib/postgresql/.ssh/id_ed25519"
    }
  }
}
EOF

}

x_backup_restore() {
  chmod 0600 /var/lib/postgresql/.ssh/id_ed25519
  x_remake_dirs

  # rerun the cluster
  echo_delim "init and run a cluster"
  xpg_rebuild
  xpg_start

  # run wal-receivers
  echo_delim "running wal-receivers"
  nohup /usr/local/bin/pgrwl start -c "/tmp/config.json" -m receive >>"$LOG_FILE" 2>&1 &

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
  pg_dumpall -f /tmp/pg_dumpall-before

  echo_delim "waiting upload"
  sleep 10

  # stop cluster, cleanup data
  echo_delim "teardown"
  pkill -9 pgrwl || true
  xpg_teardown

  # restore from backup
  echo_delim "restoring backup"
  mv "${BASEBACKUP_PATH}/data" "${PGDATA}"
  chmod 0750 "${PGDATA}"
  chown -R postgres:postgres "${PGDATA}"
  touch "${PGDATA}/recovery.signal"

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
}

x_backup_restore "${@}"
