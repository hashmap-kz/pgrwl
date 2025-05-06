#!/usr/bin/env bash
set -euo pipefail

# build binary
rm -rf bin
(
  cd ../../
  make build
  mv bin examples
)

# run container
docker compose down -v
docker compose up -d --build

# wait pg_isready
until docker exec pg-primary pg_isready -U postgres >/dev/null 2>&1; do
  echo "Waiting for PostgreSQL to be ready..."
  sleep 1
done

# run wal-receiver
chmod +x bin/pgreceivewal
./bin/pgreceivewal -D wals -S example_slot --log-devel=debug
