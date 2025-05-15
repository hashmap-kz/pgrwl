#!/usr/bin/env bash
set -euo pipefail

export PGHOST=localhost
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=postgres

go run ../main.go receive -D wals -c configs/01-basic-receive.json
