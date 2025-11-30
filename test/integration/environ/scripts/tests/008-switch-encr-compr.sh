#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/tests/utils.sh

x_remake_config() {
  cat <<EOF > "/tmp/config-zstd.yaml"
main:
  listen_port: 7070
  directory: /tmp/wal-archive
receiver:
  slot: pgrwl_v5
  uploader:
    sync_interval: 3s
    max_concurrency: 4
log:
  level: debug
  format: text
  add_source: true
storage:
  name: "local"
  compression:
    algo: zstd
EOF

  cat <<EOF > "/tmp/config-gzip-aes.yaml"
main:
  listen_port: 7070
  directory: /tmp/wal-archive
receiver:
  slot: pgrwl_v5
  uploader:
    sync_interval: 3s
    max_concurrency: 4
log:
  level: debug
  format: text
  add_source: true
storage:
  name: "local"
  compression:
    algo: gzip
  encryption:
    algo: aes-256-gcm
    pass: qwerty123
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

  # run wal-receivers (zstd compression, no encryption)
  echo_delim "running wal-receivers"
  nohup /usr/local/bin/pgrwl start -c "/tmp/config-zstd.yaml" -m receive >>"$LOG_FILE" 2>&1 &

  # make a basebackup before doing anything
  echo_delim "creating basebackup"
  pg_basebackup \
    --pgdata="${BASEBACKUP_PATH}/data" \
    --wal-method=none \
    --checkpoint=fast \
    --progress \
    --no-password \
    --verbose

  # 1) trying to write ~25 of WAL files as quick as possible
  for ((i = 0; i < 25; i++)); do
    psql -U postgres -c 'drop table if exists xxx; select pg_switch_wal(); create table if not exists xxx(id serial);' > /dev/null 2>&1
  done

  # wait compressor/encryptor
  sleep 10

  echo_delim "running wal-receiver with gzip/aes"
  sudo pkill -9 pgrwl || true
  nohup /usr/local/bin/pgrwl start -c "/tmp/config-gzip-aes.yaml" -m receive >>"$LOG_FILE" 2>&1 &

  # 2) trying to write ~25 of WAL files as quick as possible
  for ((i = 0; i < 25; i++)); do
    psql -U postgres -c 'drop table if exists xxx; select pg_switch_wal(); create table if not exists xxx(id serial);' > /dev/null 2>&1
  done

  # wait compressor/encryptor
  sleep 10

  # remember the state
  pg_dumpall -f "/tmp/pgdumpall-before" --restrict-key=0

  # stop cluster, cleanup data
  echo_delim "teardown"
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
  nohup /usr/local/bin/pgrwl start -c "/tmp/config-gzip-aes.yaml" -m serve >>"$LOG_FILE" 2>&1 &

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
  pg_dumpall -f "/tmp/pgdumpall-after" --restrict-key=0
  diff "/tmp/pgdumpall-before" "/tmp/pgdumpall-after"
}

x_backup_restore "${@}"
