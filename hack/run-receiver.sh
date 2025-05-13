#!/usr/bin/env bash
set -euo pipefail

export PGHOST=localhost
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=postgres

export PGRWL_LOG_LEVEL=trace
#export PGRWL_LISTEN_PORT=5080

# PGHOST=localhost;PGPASSWORD=postgres;PGPORT=5432;PGUSER=postgres;PGRWL_LISTEN_PORT=5080;PGRWL_LOG_LEVEL=trace;

go run ../main.go wal-receive -D wals -S pg_recwal_5
