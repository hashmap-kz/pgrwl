#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/pg/pg.sh

export PG_BASEBACKUP_PATH="/tmp/basebackup"

xpg_wait_is_ready

rm -rf "${PG_BASEBACKUP_PATH}"
mkdir -p "${PG_BASEBACKUP_PATH}"
pg_basebackup \
  --pgdata="${PG_BASEBACKUP_PATH}/data" \
  --wal-method=none \
  --checkpoint=fast \
  --progress \
  --no-password \
  --verbose
