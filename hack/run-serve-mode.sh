#!/usr/bin/env bash
set -euo pipefail

export PGRWL_DIRECTORY=wals
export PGRWL_LOG_LEVEL=debug
export PGRWL_LOG_FORMAT=text

go run ../main.go serve
