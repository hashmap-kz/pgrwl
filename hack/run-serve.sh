#!/usr/bin/env bash
set -euo pipefail

export PGRWL_LOG_LEVEL=trace

#export PGRWL_LISTEN_PORT=5080

go run ../main.go serve -D wals
