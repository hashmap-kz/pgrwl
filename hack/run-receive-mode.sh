#!/usr/bin/env bash
set -euo pipefail

export PGHOST=localhost
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=postgres

export PGRWL_LOG_LEVEL=debug
export PGRWL_LOG_FORMAT=text

go run ../main.go receive -D wals
