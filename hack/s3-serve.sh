#!/usr/bin/env bash
set -euo pipefail

go run ../main.go start -c configs/s3/serve.json
