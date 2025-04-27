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