#!/usr/bin/env bash
set -euo pipefail

go run ../main.go start -c configs/04-s3-serve.json
