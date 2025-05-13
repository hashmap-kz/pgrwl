#!/usr/bin/env bash
set -euo pipefail

export PGRWL_LOG_LEVEL=trace
export PGRWL_MODE=restore

go run ../main.go start -D wals
