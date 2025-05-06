### Usage

```
# Run compose services (postgres, wal-receiver)
make restart

# Create a base backup  
# Note: The backup is created *before* any schema or data modifications performed below.  
# Note: The backup does not include WAL files (--wal-method=none)  
make basebackup

# Generate sample data
make gendata

# Run inserts in a background
make background-inserts

# Examine the result
docker exec -it pg-primary psql -U postgres -c 'select * from public.tslog order by 1 desc limit 10;'
docker exec -it pg-primary psql -U postgres -c 'select count(*) from public.bigdata;'

# Examine the WAL archive
make show-archive

# Tear down the PostgreSQL cluster inside the container  
# This is a forceful operation: it terminates all PostgreSQL processes abruptly and removes the data directory.
make teardown

# Restore the cluster from the base backup and WAL archive
make restore

# Exec into the container and examine the result after restoration

# 1) Tail the log of background inserts and get the latest record:
docker exec -it pg-primary tail /tmp/insert-ts.log | grep -i record | sort -r

# 2) Retrieve the restored data from background inserts, ordered by timestamp:
docker exec -it pg-primary psql -U postgres -c 'select * from public.tslog order by 1 desc limit 10;'

# 3) Check the row count of the 512MiB test table:
docker exec -it pg-primary psql -U postgres -c 'select count(*) from public.bigdata;'

# Explore WAL receiver logs
docker logs --tail 10 pgreceivewal
```

### Expected result

The cluster was restored to the last successfully inserted record (`2025-05-06 20:57:22.743069` in this example).

```
# 1) Logs produced by the cluster (tail output, latest message indicating the database is restored):
#
# 2025-05-06 20:57:26 +05 [234-8]  LOG:  database system is ready to accept connections
#
# 2) Logs produced by the background insert script (tail output, ordered descending):
#
# RECORD ADDED: 2025-05-06 20:57:22.743069
# RECORD ADDED: 2025-05-06 20:57:21.715736
# RECORD ADDED: 2025-05-06 20:57:20.692507
#
# 3) Corresponding database rows (tail output, ordered descending):
#
#              ts             
# ----------------------------
#  2025-05-06 20:57:22.743069
#  2025-05-06 20:57:21.715736
#  2025-05-06 20:57:20.692507
```
