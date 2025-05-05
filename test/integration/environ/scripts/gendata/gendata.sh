#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/pg/pg.sh

export NUM_TABLES=100
export ROWS=10000

script_path="/var/lib/postgresql/scripts/gendata/generate_sql.sh"

chmod +x "${script_path}"
seq 1 "${NUM_TABLES}" | xargs -P4 -I{} "${script_path}" {} "${ROWS}"
