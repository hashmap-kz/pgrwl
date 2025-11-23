#!/bin/bash

export PG_MAJOR="${PG_MAJOR:-17}"
export PG_BIN="/usr/lib/postgresql/${PG_MAJOR}/bin"

export PGDATA="/var/lib/postgresql/${PG_MAJOR}/main/pgdata"

start_database() {
  su - postgres <<EOF
${PG_BIN}/pg_ctl \
  -D ${PGDATA} \
  -o "-c config_file=/etc/postgresql/${PG_MAJOR}/main/postgresql.conf" \
  -o "-c hba_file=/etc/postgresql/${PG_MAJOR}/main/pg_hba.conf" \
  start
EOF
}

initialize_database() {
  su - postgres <<EOF
  ${PG_BIN}/initdb -D ${PGDATA}
EOF
  start_database
}

service ssh restart

mkdir -p "${PGDATA}"
chmod 0750 "${PGDATA}"
chown -R postgres:postgres "/var/lib/postgresql"
chown -R postgres:postgres "/etc/postgresql"

mkdir -p /var/log/postgresql
chown -R postgres:postgres /var/log/postgresql

if [ ! -s "${PGDATA}/PG_VERSION" ]; then
  initialize_database
else
  start_database
fi

su - postgres <<EOF
  ${PG_BIN}/psql -c "ALTER USER postgres WITH PASSWORD 'postgres';"
EOF

tail -f /var/log/postgresql/pg.log
