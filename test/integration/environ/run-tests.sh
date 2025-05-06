#!/usr/bin/env bash
set -euo pipefail

(
  make restart

  # Wait for PostgreSQL to become ready inside the container
  until docker exec pg-primary pg_isready -U postgres >/dev/null 2>&1; do
    echo "Waiting for PostgreSQL to be ready..."
    sleep 1
  done

  docker exec -it pg-primary chmod +x /var/lib/postgresql/scripts/runners/run-tests.sh
  docker exec -it pg-primary su - postgres -c /var/lib/postgresql/scripts/runners/run-tests.sh
)
