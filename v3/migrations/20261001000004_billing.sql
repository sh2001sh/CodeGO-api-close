-- Ledger-first billing (plan §5). All amounts are micro-credits (bigint).
-- The only writer of balances is the ledger worker; nothing else updates
-- accounts.balance.

CREATE TABLE v3_billing.accounts (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_type  text        NOT NULL CHECK (owner_type IN ('user', 'api_key', 'subscription', 'platform')),
    owner_id    bigint      NOT NULL,
    kind        text        NOT NULL CHECK (kind IN (
                                'wallet',               -- user credits
                                'key_budget',           -- api_keys.budget_limited
                                'subscription',         -- per subscription period
                                'marketplace_pending',  -- owner earnings before the 24 h hold
                                'marketplace_earned',
                                'platform_revenue')),
    balance     bigint      NOT NULL DEFAULT 0,         -- denormalized from ledger_entries
    version     bigint      NOT NULL DEFAULT 0,         -- bumped on every posted entry
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_type, owner_id, kind)
);

-- Append-only. balance_after makes every entry independently auditable.
CREATE TABLE v3_billing.ledger_entries (
    id            bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id    bigint      NOT NULL REFERENCES v3_billing.accounts (id),
    amount        bigint      NOT NULL CHECK (amount <> 0), -- positive credit, negative debit
    balance_after bigint      NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(), -- fixed-width columns first: no row padding
    kind          text        NOT NULL CHECK (kind IN (
                                  'opening',            -- v2 migration opening balance
                                  'topup', 'redeem', 'reward', 'adjustment',
                                  'usage', 'refund',
                                  'subscription_grant', 'subscription_expire',
                                  'marketplace_accrue', 'marketplace_release', 'marketplace_reclaim',
                                  'transfer')),
    operation_id  text        NOT NULL UNIQUE,              -- idempotency key
    request_id    text,
    reason        text        NOT NULL DEFAULT '',
    metadata      jsonb       NOT NULL DEFAULT '{}'
);

CREATE INDEX ledger_entries_account_idx ON v3_billing.ledger_entries (account_id, id DESC);
CREATE INDEX ledger_entries_request_idx ON v3_billing.ledger_entries (request_id) WHERE request_id IS NOT NULL;

-- Durable record of Redis reservations; state transitions are single UPDATEs
-- guarded by "WHERE state = $from" (plan §5 state machine).
-- Column order is fixed-width first to avoid per-row padding (Atlas PG110).
CREATE TABLE v3_billing.reservations (
    account_id  bigint      NOT NULL REFERENCES v3_billing.accounts (id),
    amount      bigint      NOT NULL CHECK (amount >= 0),
    settled     bigint      CHECK (settled >= 0),
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    closed_at   timestamptz,
    request_id  text        PRIMARY KEY,
    state       text        NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'settled', 'released', 'expired')),
    CHECK ((state = 'open') = (closed_at IS NULL))
);

CREATE INDEX reservations_open_expiry_idx ON v3_billing.reservations (expires_at) WHERE state = 'open';

-- Replay protection for billing events (borrowed from sub2api): same request
-- and fingerprint is a duplicate; same request with a different fingerprint
-- is a conflict that goes to manual review.
CREATE TABLE v3_billing.billing_dedup (
    account_id  bigint      NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    request_id  text        NOT NULL,
    fingerprint bytea       NOT NULL CHECK (octet_length(fingerprint) = 32),
    PRIMARY KEY (request_id, account_id)
);

CREATE INDEX billing_dedup_created_idx ON v3_billing.billing_dedup (created_at);

CREATE TABLE v3_billing.dedup_conflicts (
    id                   bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    request_id           text        NOT NULL,
    account_id           bigint      NOT NULL,
    existing_fingerprint bytea       NOT NULL,
    incoming_fingerprint bytea       NOT NULL,
    payload              jsonb       NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    resolved_at          timestamptz
);

-- Cutover routing (plan §9): which gateway owns an account's money.
CREATE TABLE v3_billing.account_engine (
    user_id    bigint      PRIMARY KEY,
    engine     text        NOT NULL DEFAULT 'v2' CHECK (engine IN ('v2', 'migrating', 'v3')),
    since      timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER accounts_touch BEFORE UPDATE ON v3_billing.accounts
    FOR EACH ROW EXECUTE FUNCTION v3_platform.touch_updated_at();
CREATE TRIGGER account_engine_invalidate AFTER INSERT OR UPDATE OR DELETE ON v3_billing.account_engine
    FOR EACH ROW EXECUTE FUNCTION v3_platform.enqueue_invalidation('account_engine', 'user_id');
