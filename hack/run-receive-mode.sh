#!/usr/bin/env bash
set -euo pipefail

export PGHOST=localhost
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=postgres

export PGRWL_LOG_LEVEL=trace
export PGRWL_MODE=receive

# PGHOST=localhost;PGPORT=5432;PGUSER=postgres;PGPASSWORD=postgres;PGRWL_LOG_LEVEL=trace;

go run ../main.go start -D wals -S pg_recwal_5
