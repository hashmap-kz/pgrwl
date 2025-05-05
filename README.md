```
START_REPLICATION SLOT walg_test_slot PHYSICAL 0/4C000000 TIMELINE 1
```

```
synchronous_standby_names

It defines which standbys must acknowledge WAL writes before the primary considers a commit to be durable.

If set, PostgreSQL waits for WAL replication confirmation from one or more standbys.

The idea is: no data loss, because commit waits until replicas confirm.

In short:
PostgreSQL will block the client until the WAL is safely written to the standbys you list in synchronous_standby_names.
```

```
synchronous_standby_names = 'pg_receivewal,pg_recwal_5'

# to 'force' keepalive messages
wal_sender_timeout = 5000  # 5 seconds
```

### Gen data
```
-- Each row = ~1KiB
-- 4 million rows = ~4GiB total size (with overhead)
-- /usr/lib/postgresql/17/bin/pgbench -i -s 256 postgres

CREATE TABLE bigdata AS
SELECT i, repeat('x', 1024) AS filler
FROM generate_series(1, 4 * 1024 * 1024) AS t(i);
```
