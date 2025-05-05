#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/pg/pg.sh

TABLE=$(printf "rand_table_%03d" "${1}")
ROWS="$2"

psql -U postgres -d postgres <<EOF
DROP TABLE IF EXISTS $TABLE;
CREATE TABLE $TABLE AS
SELECT
  g AS id,
  md5(random()::text) AS col1,
  md5(random()::text) AS col2,
  md5(random()::text) AS col3
FROM generate_series(1, $ROWS) AS g;
EOF
