#!/usr/bin/env bash
set -euo pipefail

# build binary
rm -rf bin
(
  cd ../
  make build
  mv bin examples
)

rm -rf wals

# run container
docker compose down -v
docker compose up -d --build

# # wait until cluster is ready
# until docker exec pg-primary pg_isready -U postgres >/dev/null 2>&1; do
#   echo "Waiting for PostgreSQL to be ready..."
#   sleep 1
# done
#
# # setup env-vars
# export PGHOST='localhost'
# export PGPORT='5432'
# export PGUSER='postgres'
# export PGPASSWORD='postgres'
#
# # run wal-receiver
# chmod +x bin/pgreceivewal
# ./bin/pgreceivewal -D wals -S example_slot
