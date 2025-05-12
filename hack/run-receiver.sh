#!/usr/bin/env bash
set -euo pipefail

export PGHOST=localhost
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=postgres

export PGRWL_LOG_LEVEL=trace

#export PGRWL_HTTP_SERVER_ADDR=127.0.0.1:5080
#export PGRWL_HTTP_SERVER_TOKEN=pgrwladmin

# PGHOST=localhost;PGPASSWORD=postgres;PGPORT=5432;PGUSER=postgres;PGRWL_HTTP_SERVER_ADDR=:5080;PGRWL_HTTP_SERVER_TOKEN=1024;PGRWL_LOG_LEVEL=trace;

go run ../main.go wal-receive -D wals -S pg_recwal_5
