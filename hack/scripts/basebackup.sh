#!/usr/bin/env bash
set -euo pipefail

export PGHOST=localhost
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=postgres

pg_basebackup \
  --pgdata="data" \
  --wal-method=none \
  --checkpoint=fast \
  --progress \
  --no-password \
  --verbose
