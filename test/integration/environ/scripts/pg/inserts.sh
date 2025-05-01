export PGHOST="localhost"
export PGPORT="5432"
export PGUSER="postgres"
export PGPASSWORD="postgres"
export LOG_FILE="/tmp/insertts.log"

psql -c "drop table if exists public.tslog;"
psql -c "create table if not exists public.tslog (ts TIMESTAMP DEFAULT now());"

while true; do
  psql -c "INSERT INTO public.tslog DEFAULT VALUES;" >>"$LOG_FILE" 2>&1
  sleep 1
done
