-- PostgreSQL only. Explicit maintenance operation; never run on application start.
-- psql -v ON_ERROR_STOP=1 -f scripts/marketplace-income-hourly.sql
-- The one-time backfill blocks settlement writes (not reads). A busy lock fails
-- after 2s; each statement is limited to 120s. Any error rolls back installation.
-- Plain CREATE intentionally refuses to rebuild an already installed projection.
BEGIN;
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '120s';
LOCK TABLE marketplace.settlements IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE marketplace.income_hourly_totals (
    owner_user_id bigint NOT NULL,
    group_id text NOT NULL,
    bucket_start bigint NOT NULL,
    request_count bigint NOT NULL,
    total_income bigint NOT NULL,
    pending_income bigint NOT NULL,
    released_income bigint NOT NULL,
    reclaimed_income bigint NOT NULL,
    forfeited_income bigint NOT NULL,
    PRIMARY KEY (owner_user_id, group_id, bucket_start)
);
CREATE INDEX income_hourly_bucket ON marketplace.income_hourly_totals (bucket_start);
CREATE INDEX income_hourly_group ON marketplace.income_hourly_totals (group_id, owner_user_id);

INSERT INTO marketplace.income_hourly_totals
SELECT owner_user_id, group_id, floor(extract(epoch FROM created_at)/3600)::bigint*3600,
    COUNT(*), SUM(owner_net_amount),
    SUM(CASE WHEN status='pending' THEN owner_net_amount ELSE 0 END),
    SUM(CASE WHEN status='released' THEN owner_net_amount-reclaimed_amount ELSE 0 END),
    SUM(CASE WHEN status='reclaimed' THEN owner_net_amount ELSE reclaimed_amount END),
    SUM(CASE WHEN status='forfeited' THEN owner_net_amount ELSE 0 END)
FROM marketplace.settlements GROUP BY 1,2,3;

-- Writers only append deltas, avoiding a new summary-row/wallet lock order.
-- A single read snapshot sees totals plus every committed delta, including
-- updates not yet compacted by the release worker.
CREATE TABLE marketplace.income_hourly_delta (
    LIKE marketplace.income_hourly_totals INCLUDING DEFAULTS,
    id bigserial PRIMARY KEY
);
CREATE INDEX income_delta_owner ON marketplace.income_hourly_delta (owner_user_id, group_id, bucket_start);
CREATE INDEX income_delta_group ON marketplace.income_hourly_delta (group_id);
CREATE INDEX income_delta_bucket ON marketplace.income_hourly_delta (bucket_start);
CREATE VIEW marketplace.income_hourly AS
SELECT owner_user_id, group_id, bucket_start, request_count, total_income,
    pending_income, released_income, reclaimed_income, forfeited_income FROM marketplace.income_hourly_totals
UNION ALL
SELECT owner_user_id, group_id, bucket_start, request_count, total_income,
    pending_income, released_income, reclaimed_income, forfeited_income FROM marketplace.income_hourly_delta;

-- Transition tables aggregate a whole statement: one reclaim batch appends
-- one delta per affected bucket, rather than one delta per settlement.
CREATE FUNCTION marketplace.update_income_hourly() RETURNS trigger
LANGUAGE plpgsql AS $function$
DECLARE source text;
BEGIN
    IF TG_OP = 'INSERT' THEN
        source := 'SELECT 1 AS sign, n.* FROM new_income n';
    ELSIF TG_OP = 'DELETE' THEN
        source := 'SELECT -1 AS sign, o.* FROM old_income o';
    ELSE
        source := 'SELECT 1 AS sign, n.* FROM new_income n UNION ALL SELECT -1 AS sign, o.* FROM old_income o';
    END IF;
    EXECUTE '
        INSERT INTO marketplace.income_hourly_delta (owner_user_id, group_id, bucket_start, request_count, total_income, pending_income, released_income, reclaimed_income, forfeited_income)
        SELECT owner_user_id, group_id, floor(extract(epoch FROM created_at)/3600)::bigint*3600,
            SUM(sign), SUM(sign * owner_net_amount),
            SUM(sign * CASE WHEN status=''pending'' THEN owner_net_amount ELSE 0 END),
            SUM(sign * CASE WHEN status=''released'' THEN owner_net_amount-reclaimed_amount ELSE 0 END),
            SUM(sign * CASE WHEN status=''reclaimed'' THEN owner_net_amount ELSE reclaimed_amount END),
            SUM(sign * CASE WHEN status=''forfeited'' THEN owner_net_amount ELSE 0 END)
        FROM (' || source || ') changes
        GROUP BY 1,2,3';
    RETURN NULL;
END
$function$;

CREATE TRIGGER income_hourly_insert AFTER INSERT ON marketplace.settlements
REFERENCING NEW TABLE AS new_income FOR EACH STATEMENT
EXECUTE FUNCTION marketplace.update_income_hourly();
CREATE TRIGGER income_hourly_update AFTER UPDATE ON marketplace.settlements
REFERENCING OLD TABLE AS old_income NEW TABLE AS new_income FOR EACH STATEMENT
EXECUTE FUNCTION marketplace.update_income_hourly();
CREATE TRIGGER income_hourly_delete AFTER DELETE ON marketplace.settlements
REFERENCING OLD TABLE AS old_income FOR EACH STATEMENT
EXECUTE FUNCTION marketplace.update_income_hourly();

-- Delete and merge exactly the rows selected, in one transaction. SKIP LOCKED
-- permits multiple workers; no high-water ID can skip an uncommitted writer.
CREATE FUNCTION marketplace.compact_income_hourly(batch_size integer) RETURNS bigint
LANGUAGE sql AS $compact$
WITH selected AS MATERIALIZED (
    SELECT id FROM marketplace.income_hourly_delta ORDER BY id
    LIMIT batch_size FOR UPDATE SKIP LOCKED
), moved AS (
    DELETE FROM marketplace.income_hourly_delta d USING selected s WHERE d.id=s.id RETURNING d.*
), merged AS (
    INSERT INTO marketplace.income_hourly_totals AS h
    SELECT owner_user_id, group_id, bucket_start,
        SUM(request_count), SUM(total_income), SUM(pending_income),
        SUM(released_income), SUM(reclaimed_income), SUM(forfeited_income)
    FROM moved GROUP BY 1,2,3 ORDER BY 1,2,3
    ON CONFLICT (owner_user_id, group_id, bucket_start) DO UPDATE SET
        request_count = h.request_count + EXCLUDED.request_count,
        total_income = h.total_income + EXCLUDED.total_income,
        pending_income = h.pending_income + EXCLUDED.pending_income,
        released_income = h.released_income + EXCLUDED.released_income,
        reclaimed_income = h.reclaimed_income + EXCLUDED.reclaimed_income,
        forfeited_income = h.forfeited_income + EXCLUDED.forfeited_income
    RETURNING 1
)
SELECT COUNT(*) FROM moved;
$compact$;
ANALYZE marketplace.income_hourly_totals;
COMMIT;
