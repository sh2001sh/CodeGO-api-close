-- Business ledger postings carry a durable Redis delivery record in the same
-- transaction. The balance column remains a projection owned by the ledger.
CREATE TABLE v3_billing.balance_outbox (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id bigint NOT NULL REFERENCES v3_billing.accounts(id),
    amount bigint NOT NULL CHECK (amount <> 0),
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    operation_id text NOT NULL UNIQUE
);

CREATE TABLE v3_billing.usage_logs (
    id bigint GENERATED ALWAYS AS IDENTITY,
    created_at timestamptz NOT NULL,
    account_id bigint NOT NULL REFERENCES v3_billing.accounts(id),
    user_id bigint NOT NULL DEFAULT 0,
    key_id bigint NOT NULL DEFAULT 0,
    channel_id bigint NOT NULL DEFAULT 0,
    credential_id bigint NOT NULL DEFAULT 0,
    amount bigint NOT NULL CHECK (amount >= 0),
    prompt_tokens bigint NOT NULL DEFAULT 0 CHECK (prompt_tokens >= 0),
    completion_tokens bigint NOT NULL DEFAULT 0 CHECK (completion_tokens >= 0),
    cached_tokens bigint NOT NULL DEFAULT 0 CHECK (cached_tokens >= 0),
    estimated boolean NOT NULL DEFAULT false,
    request_id text NOT NULL,
    model text NOT NULL DEFAULT '',
    terminal text NOT NULL DEFAULT '',
    PRIMARY KEY (created_at, id),
    UNIQUE (created_at, request_id, account_id)
) PARTITION BY RANGE (created_at);

CREATE TABLE v3_billing.usage_logs_default PARTITION OF v3_billing.usage_logs DEFAULT;
CREATE INDEX usage_logs_user_page_idx ON v3_billing.usage_logs (user_id, created_at DESC, id DESC);
CREATE INDEX usage_logs_channel_page_idx ON v3_billing.usage_logs (channel_id, created_at DESC, id DESC);
-- New partitions are created before each month begins by EnsureUsagePartitions;
-- DEFAULT retains late historical events instead of rejecting a charged request.
