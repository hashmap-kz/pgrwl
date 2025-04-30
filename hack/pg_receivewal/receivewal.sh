export PGHOST=localhost
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=postgres

mkdir wals

pg_receivewal \
  --no-password \
  --slot=pg_receivewal \
  --create-slot \
  --if-not-exists

pg_receivewal \
  --dbname='dbname=replication options=-cdatestyle=iso replication=true application_name=pg_receivewal' \
  --verbose \
  --no-password \
  --directory=wals \
  --slot=pg_receivewal \
  --synchronous
