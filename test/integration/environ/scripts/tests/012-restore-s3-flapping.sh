#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/tests/utils.sh

x_remake_config() {
  cat <<EOF > "/tmp/config.json"
{
  "main": {
     "listen_port": 7070,
     "directory": "${WAL_PATH}"
  },
  "receiver": {
     "slot": "pgrwl_v5",
     "no_loop": true,
     "uploader": {
       "sync_interval": "2s",
       "max_concurrency": 4
     }
  },
  "log": {
    "level": "${LOG_LEVEL_DEFAULT}",
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

x_backup_restore_with_flapping_minio() {
  echo_delim "cleanup state"
  x_remake_dirs
  x_remake_config
  x_wait_minio_up

  echo_delim "init and run a cluster"
  xpg_rebuild
  xpg_start

  echo_delim "running wal receiver"
  x_start_receiver "/tmp/config.json"

  echo_delim "creating base backup"
  /usr/local/bin/pgrwl backup -c "/tmp/config.json"

  chmod +x "${BACKGROUND_INSERTS_SCRIPT_PATH}"
  nohup "${BACKGROUND_INSERTS_SCRIPT_PATH}" >>"${BACKGROUND_INSERTS_SCRIPT_LOG_FILE}" 2>&1 &

  echo_delim "generate load while minio flaps"
  x_minio_flap 1 2 3
  pgbench -i -s 10 postgres
  x_generate_wal 50
  sleep 10

  pkill -f inserts.sh || true

  echo_delim "save expected state"
  pg_dumpall -f "/tmp/pgdumpall-before" --restrict-key=0

  echo_delim "teardown original cluster"
  x_stop_receiver
  xpg_teardown

  echo_delim "restore base backup while minio flaps again"
  x_minio_flap 1 2 3
  /usr/local/bin/pgrwl restore --dest="${PGDATA}" -c "/tmp/config.json"

  chmod 0750 "${PGDATA}"
  chown -R postgres:postgres "${PGDATA}"
  touch "${PGDATA}/recovery.signal"

  xpg_config
  cat <<EOF >>"${PG_CFG}"
restore_command = 'pgrwl restore-command --serve-addr=127.0.0.1:7070 %f %p'
EOF

  echo_delim "start wal serving"
  x_start_serving "/tmp/config.json"

  >/var/log/postgresql/pg.log

  echo_delim "start restored cluster"
  xpg_start

  xpg_wait_is_in_recovery
  cat /var/log/postgresql/pg.log

  echo_delim "compare before and after"
  pg_dumpall -f "/tmp/pgdumpall-after" --restrict-key=0
  diff "/tmp/pgdumpall-before" "/tmp/pgdumpall-after"

  echo_delim "show latest applied records"
  psql --pset pager=off -c "select * from public.tslog;"
  tail -10 "${BACKGROUND_INSERTS_SCRIPT_LOG_FILE}" || true
}

x_backup_restore_with_flapping_minio "$@"
