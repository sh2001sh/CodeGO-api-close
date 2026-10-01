-- Original ledger evidence is separate from current account openings: reading
-- or importing it must never debit current money, advance versions, or enqueue
-- another balance delivery. Original identifiers and nullable balances survive.
CREATE TABLE v3_billing.historical_accounts (
    source_account_id text PRIMARY KEY,
    account_id bigint REFERENCES v3_billing.accounts(id),
    owner_type text NOT NULL,
    owner_id bigint NOT NULL,
    account_type text NOT NULL,
    unit text NOT NULL,
    status text NOT NULL,
    version bigint NOT NULL,
    metadata jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE INDEX historical_accounts_owner_idx ON v3_billing.historical_accounts(owner_type,owner_id);
CREATE INDEX historical_accounts_mapped_idx ON v3_billing.historical_accounts(account_id);

CREATE TABLE v3_billing.historical_entries (
    entry_id text PRIMARY KEY,
    source_account_id text NOT NULL REFERENCES v3_billing.historical_accounts(source_account_id),
    reference_type text NOT NULL,
    reference_id text NOT NULL,
    entry_type text NOT NULL,
    direction text NOT NULL CHECK(direction IN ('debit','credit')),
    amount bigint NOT NULL,
    balance_after bigint,
    idempotency_key text NOT NULL UNIQUE,
    reason_code text NOT NULL,
    reason_detail text NOT NULL,
    operator_type text NOT NULL,
    operator_id text NOT NULL,
    metadata jsonb NOT NULL,
    created_at timestamptz NOT NULL
);
CREATE INDEX historical_entries_account_date_idx ON v3_billing.historical_entries(source_account_id,created_at DESC,entry_id);
