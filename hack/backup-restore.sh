#!/usr/bin/env bash
# set -euo pipefail

. pg.sh

xpg_basebackup
xpg_teardown

mv "${BB_TARGET}" "${PGDATA}"

chmod 0750 "${PGDATA}"
chown -R postgres:postgres "${PGDATA}"
touch "${PGDATA}/recovery.signal"

cat <<EOF >>"${PGDATA}/postgresql.auto.conf"
restore_command = true
EOF

systemctl start postgresql@17-main.service
