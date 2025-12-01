#!/usr/bin/env bash
set -euo pipefail

export PGRWL_DAEMON_MODE=serve

go run ../main.go daemon -c configs/localfs/receive.yml
