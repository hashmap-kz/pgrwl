#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/utils/execs.sh

cat <<EOF >/tmp/config.json
{
  "PGDATA": "/var/lib/postgresql/17/main/pgdata",
  "PG_FETCH_TYPE": "local",
  "REPO_PATH": "/tmp/backups/01/plain",
  "REPO_TYPE": "local"
}
EOF

x_backup_restore "/tmp/config.json"
