#!/bin/bash
set -euo pipefail

APP_NAME="ts-inserts"
APP_PATH="/var/lib/postgresql/scripts/pg/inserts.sh"

ARGS=(
  "placeholder"
)

chmod +x "${APP_PATH}"
source "/var/lib/postgresql/scripts/pg/run_app.sh" "$@"
