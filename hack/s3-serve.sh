#!/usr/bin/env bash
set -euo pipefail

export PGRWL_DAEMON_MODE=serve

go run ../cmd/pgrwl/main.go daemon -c configs/s3/receive.yml
