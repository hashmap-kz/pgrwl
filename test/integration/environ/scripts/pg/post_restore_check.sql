-- https://www.postgresql.org/docs/current/amcheck.html#AMCHECK-FUNCTIONS

\set ON_ERROR_STOP on

\echo '============================================================'
\echo 'PostgreSQL post-restore consistency check'
\echo '============================================================'

CREATE EXTENSION IF NOT EXISTS amcheck;

\echo ''
\echo 'Checking whether recovery is finished...'

DO $$
BEGIN
    IF pg_is_in_recovery() THEN
        RAISE EXCEPTION 'PostgreSQL is still in recovery';
    END IF;
END $$;

\echo 'OK: server is not in recovery'

\echo ''
\echo 'Checking heap/table consistency with verify_heapam...'

CREATE TEMP TABLE restore_heapam_errors AS
SELECT
    current_database() AS database_name,
    n.nspname AS schema_name,
    c.relname AS relation_name,
    c.oid::regclass AS relation_regclass,
    v.blkno,
    v.offnum,
    v.attnum,
    v.msg
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
CROSS JOIN LATERAL verify_heapam(
    relation      => c.oid,
    on_error_stop => false,
    check_toast   => true
) AS v
WHERE c.relkind IN ('r', 'm')
  AND c.relpersistence != 't'
ORDER BY n.nspname, c.relname, v.blkno, v.offnum;

DO $$
DECLARE
    err_count bigint;
BEGIN
    SELECT count(*) INTO err_count FROM restore_heapam_errors;

    IF err_count > 0 THEN
        RAISE EXCEPTION 'verify_heapam found % heap/table consistency error(s). Query restore_heapam_errors for details.', err_count;
    END IF;
END $$;

\echo 'OK: heap/table consistency check passed'

\echo ''
\echo 'Checking btree index consistency with bt_index_check...'

SELECT
    bt_index_check(
        index          => c.oid,
        heapallindexed => true
    ),
    current_database() AS database_name,
    n.nspname AS schema_name,
    c.relname AS index_name,
    c.oid::regclass AS index_regclass,
    c.relpages
FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_am am ON am.oid = c.relam
WHERE am.amname = 'btree'
  AND c.relkind = 'i'
  AND c.relpersistence != 't'
  AND i.indisready
  AND i.indisvalid
ORDER BY c.relpages DESC;

\echo 'OK: btree index consistency check passed'

\echo ''
\echo '============================================================'
\echo 'SUCCESS: post-restore consistency check passed'
\echo '============================================================'
