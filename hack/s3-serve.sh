#!/usr/bin/env bash
set -euo pipefail

export PGRWL_MODE=serve

go run ../main.go start -c configs/s3/receive.yml
