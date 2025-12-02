#!/usr/bin/env bash
set -euo pipefail

export PGHOST=localhost
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=postgres
export PGRWL_DAEMON_MODE=backup

go run ../main.go daemon -c configs/localfs/backup-time-retention.yml
