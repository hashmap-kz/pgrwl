#!/usr/bin/env bash
set -euo pipefail

export PGRWL_LOG_LEVEL=debug
export PGRWL_LOG_FORMAT=text

go run ../main.go serve -D wals
