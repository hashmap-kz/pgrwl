#!/usr/bin/env bash
set -euo pipefail

export PGRWL_LOG_LEVEL=trace

#export PGRWL_HTTP_SERVER_ADDR=127.0.0.1:5080
#export PGRWL_HTTP_SERVER_TOKEN=pgrwladmin

go run ../main.go serve -D wals
