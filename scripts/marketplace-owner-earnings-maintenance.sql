-- Run with psql outside a transaction. Regular VACUUM keeps the existing
-- owner/group covering index from fetching old settlement heap pages.
-- At 11M rows the default 20% threshold leaves millions of changes untreated.
-- No settlement amounts, statuses, or account balances are modified here.
\set ON_ERROR_STOP on
SET lock_timeout = '3s';
ALTER TABLE marketplace.settlements SET (
    autovacuum_vacuum_scale_factor = 0.005,
    autovacuum_vacuum_threshold = 2000
);
VACUUM (ANALYZE, PARALLEL 0) marketplace.settlements;
