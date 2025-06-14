#!/usr/bin/env bash
set -euo pipefail

export PGHOST=localhost
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=postgres
export PGRWL_MODE=backup

go run ../main.go start -c configs/localfs/backup-count-retention.yml
