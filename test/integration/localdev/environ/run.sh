#!/usr/bin/env bash
set -euo pipefail

docker compose down -v
docker compose up -d --build
