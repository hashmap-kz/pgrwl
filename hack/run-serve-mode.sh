#!/usr/bin/env bash
set -euo pipefail

go run ../main.go serve -D wals -c configs/02-basic-serve.json
