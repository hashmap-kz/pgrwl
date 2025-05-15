#!/usr/bin/env bash
set -euo pipefail

go run ../main.go serve -c configs/02-basic-serve.json
