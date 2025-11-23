#!/bin/bash
set -euo pipefail

export PGHOST="localhost"
export PGPORT="5432"
export PGUSER="postgres"
export PGPASSWORD="postgres"

psql -v ON_ERROR_STOP=1 -c "drop table if exists public.tslog;"
psql -v ON_ERROR_STOP=1 -c "create table if not exists public.tslog (ts TIMESTAMP DEFAULT now());"

while true; do
  ts="$(date "+%Y-%m-%d %H:%M:%S.%6N")"
  psql -v ON_ERROR_STOP=1 -c "INSERT INTO public.tslog(ts) VALUES('${ts}');"
  echo "${ts}"
  sleep 1
done
