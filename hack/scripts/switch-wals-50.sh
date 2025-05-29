#!/usr/bin/env bash
set -euo pipefail

for (( i=0;i<50;i++ )); do
  psql -U postgres -c 'drop table if exists xxx; select pg_switch_wal(); create table if not exists xxx (id serial);';
done
