-- Run with psql on the log database, outside a transaction.
-- Cover the market ranking window without fetching every matching log row.
-- If interrupted, check pg_index.indisvalid and REINDEX INDEX CONCURRENTLY
-- before considering the index installed; IF NOT EXISTS cannot repair it.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_logs_consumer_window
    ON public.logs (created_at, channel_id, user_id) WHERE type = 2;
