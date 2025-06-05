#!/usr/bin/env bash
set -euo pipefail

export PGHOST=localhost
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=postgres

# test/integration/environ
export PGRWL_MINIO_URL="https://localhost:9000"

go run ../main.go restore --id=20060102150405 --dest=backups -c configs/s3/receive.yml
