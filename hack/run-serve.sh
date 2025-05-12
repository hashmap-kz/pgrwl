#!/usr/bin/env bash
set -euo pipefail

export PGRWL_LOG_LEVEL=trace
export PGRWL_HTTP_SERVER_ADDR=:5080
export PGRWL_HTTP_SERVER_TOKEN=1024

go run ../main.go serve -D wals
