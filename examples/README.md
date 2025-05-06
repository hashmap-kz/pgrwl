# Usage

```
### Run compose services (postgres, wal-receiver)
make restart

### Examine logs output
make logs

### Create basebackup
make basebackup

### Add some data
make gendata

### Examine result
docker exec -it pg-primary psql -U postgres -c 'select count(*) from public.bigdata;'

### Examine wal-archive
make show-archive

### Teardown PostgreSQL cluster inside container 
# It is a force operation, it terminates all postgres processes, and removes data directory.
make teardown

### Restore cluster from basebackup and a wal-archive
make restore

### Exec into container, examine result after restoration
docker exec -it pg-primary psql -U postgres -c 'select count(*) from public.bigdata;'
```
