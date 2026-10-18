-- Run once with psql outside a transaction. If the index already exists,
-- verify its definition and validity before continuing with maintenance.
-- Regular VACUUM keeps covering scans from fetching old heap pages.
-- At 11M rows the default 20% threshold leaves millions of changes untreated.
-- No settlement amounts, statuses, or account balances are modified here.
\set ON_ERROR_STOP on
SET lock_timeout = '3s';
-- Key columns allow B-tree deduplication of repeated charge amounts. Avoid
-- INCLUDE: it disables deduplication and expands the high-frequency scan.
CREATE INDEX CONCURRENTLY idx_marketplace_channel_earnings
    ON marketplace.settlements
    (group_id, owner_user_id, status, owner_net_amount, reclaimed_amount);
ALTER TABLE marketplace.settlements SET (
    autovacuum_vacuum_scale_factor = 0.005,
    autovacuum_vacuum_threshold = 2000
);
VACUUM (ANALYZE, PARALLEL 0) marketplace.settlements;
