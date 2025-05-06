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
docker exec -it pg-primary psql -U postgres
```