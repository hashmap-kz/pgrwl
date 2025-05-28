#!/usr/bin/env bash
set -euo pipefail
. /var/lib/postgresql/scripts/pg/pg.sh

echo_delim "teardown cluster, cleanup data"
xpg_teardown
