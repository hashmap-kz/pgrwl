# Usage

```
# Run compose services (postgres, wal-receiver)
make restart

# Create basebackup
make basebackup

# Add some data
make gendata

# Run inserts in a background
make background-inserts

# Examine result
docker exec -it pg-primary psql -U postgres -c 'select * from public.tslog order by 1 desc limit 10;'
docker exec -it pg-primary psql -U postgres -c 'select count(*) from public.bigdata;'

# Examine wal-archive
make show-archive

# Teardown PostgreSQL cluster inside container 
# It is a force operation, it terminates all postgres processes, and removes data directory.
make teardown

# Restore cluster from basebackup and a wal-archive
make restore

# Exec into container, examine result after restoration
# 1) Tail a log of background inserts, get the latest one:
docker exec -it pg-primary tail /tmp/insert-ts.log | grep -i record | sort -r
# 2) Get the restored data from background inserts, order by latest
docker exec -it pg-primary psql -U postgres -c 'select * from public.tslog order by 1 desc limit 10;'
# 3) Examine the count of a 512Mi table: 
docker exec -it pg-primary psql -U postgres -c 'select count(*) from public.bigdata;'

# Explore wal-receiver logs
docker logs --tail 10 pgreceivewal
```
