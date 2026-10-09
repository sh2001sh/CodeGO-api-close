-- Current balances and funding attribution still migrate in full. Only the
-- read-only legacy ledger remains in its identity-bound archive database.
CREATE TABLE v3_billing.ledger_history_archive (
    singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
    source_cluster_id text NOT NULL,
    source_database_oid bigint NOT NULL CHECK(source_database_oid > 0),
    source_database text NOT NULL,
    entry_count bigint NOT NULL CHECK(entry_count >= 0),
    archived_at timestamptz NOT NULL
);
