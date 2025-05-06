#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/pg/pg.sh

echo_delim "generate 512Mi of data"
psql -v ON_ERROR_STOP=1 -U postgres <<-EOSQL
  -- 1Gi/2
  CREATE TABLE bigdata AS
  SELECT i, repeat('x', 1024) AS filler
  FROM generate_series(1, (1 * 1024 * 1024)/2) AS t(i);
EOSQL
