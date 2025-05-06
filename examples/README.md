# Usage

### Run compose services (postgres, wal-receiver)

```
make restart
```

### Examine logs output

```
make logs
```

### Create basebackup

```
make basebackup
```

### Add some data

```
make gendata
```

### Examine result

```
docker exec -it pg-primary psql -U postgres -c 'select count(*) from public.bigdata;'
```

### Teardown PostgreSQL cluster inside container

```
make teardown
```

### Restore from basebackup and a wal-archive

```
make restore
```

### Exec into container, examine result

```
docker exec -it pg-primary psql -U postgres -c 'select count(*) from public.bigdata;'
```