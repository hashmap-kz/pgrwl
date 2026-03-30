CREATE OR REPLACE FUNCTION measure_reach_time_pair(
    p_label text,
    p_target_lsn pg_lsn,
    p_pgrwl_app text,
    p_pgrcv_app text,
    p_timeout_sec double precision,
    p_poll_interval_sec double precision,
    p_timing_delta_fail_sec double precision
)
    RETURNS TABLE
            (
                milestone        text,
                target_lsn       pg_lsn,
                pgrwl_at         timestamptz,
                pg_receivewal_at timestamptz,
                delta_sec        double precision,
                ok               boolean
            )
    LANGUAGE plpgsql
AS
$$
DECLARE
    v_start     timestamptz := clock_timestamp();
    v_now       timestamptz;
    v_t_pgrwl   timestamptz;
    v_t_pgrcv   timestamptz;
    v_lsn_pgrwl pg_lsn;
    v_lsn_pgrcv pg_lsn;
    v_delta_sec double precision;
BEGIN
    LOOP
        v_now := clock_timestamp();

        SELECT max(flush_lsn) FILTER (WHERE application_name = p_pgrwl_app),
               max(flush_lsn) FILTER (WHERE application_name = p_pgrcv_app)
        INTO
            v_lsn_pgrwl,
            v_lsn_pgrcv
        FROM pg_stat_replication;

        IF v_t_pgrwl IS NULL
            AND v_lsn_pgrwl IS NOT NULL
            AND pg_wal_lsn_diff(v_lsn_pgrwl, p_target_lsn) >= 0
        THEN
            v_t_pgrwl := v_now;
        END IF;

        IF v_t_pgrcv IS NULL
            AND v_lsn_pgrcv IS NOT NULL
            AND pg_wal_lsn_diff(v_lsn_pgrcv, p_target_lsn) >= 0
        THEN
            v_t_pgrcv := v_now;
        END IF;

        EXIT WHEN v_t_pgrwl IS NOT NULL AND v_t_pgrcv IS NOT NULL;

        IF extract(epoch FROM (v_now - v_start)) > p_timeout_sec THEN
            RAISE EXCEPTION
                'timeout waiting for apps to reach target_lsn %, pgrwl_app=%, pgrcv_app=%, last_pgrwl_lsn=%, last_pgrcv_lsn=%',
                p_target_lsn, p_pgrwl_app, p_pgrcv_app, v_lsn_pgrwl, v_lsn_pgrcv;
        END IF;

        PERFORM pg_sleep(p_poll_interval_sec);
    END LOOP;

    v_delta_sec := abs(extract(epoch FROM (v_t_pgrwl - v_t_pgrcv))::double precision);

    RETURN QUERY
        SELECT p_label,
               p_target_lsn,
               v_t_pgrwl,
               v_t_pgrcv,
               v_delta_sec,
               v_delta_sec <= p_timing_delta_fail_sec;
END;
$$;