-- Current monetary provenance and owner-spendable reward transfer holds.
-- Imported records are evidence only: this migration never credits accounts.
CREATE TABLE v3_billing.funding_source_policies (
    revenue_multiplier_ppm bigint NOT NULL CHECK (revenue_multiplier_ppm >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    source text PRIMARY KEY CHECK (source IN ('topup','blind_box','subscription','legacy_unattributed','other'))
);

CREATE TABLE v3_billing.funding_lots (
    account_id bigint REFERENCES v3_billing.accounts(id),
    original_amount bigint NOT NULL CHECK (original_amount > 0),
    remaining_amount bigint NOT NULL CHECK (remaining_amount >= 0 AND remaining_amount <= original_amount),
    revenue_multiplier_ppm bigint NOT NULL CHECK (revenue_multiplier_ppm >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    lot_id text PRIMARY KEY CHECK (lot_id <> ''),
    source_account_id text NOT NULL CHECK (source_account_id <> ''),
    source text NOT NULL CHECK (source IN ('topup','blind_box','subscription','legacy_unattributed','other')),
    reference_type text NOT NULL DEFAULT '',
    reference_id text NOT NULL DEFAULT '',
    idempotency_key text NOT NULL UNIQUE CHECK (idempotency_key <> '')
);
CREATE INDEX funding_lots_fifo_idx ON v3_billing.funding_lots(account_id,created_at,lot_id) WHERE remaining_amount > 0;

CREATE TABLE v3_billing.funding_allocations (
    account_id bigint REFERENCES v3_billing.accounts(id),
    amount bigint NOT NULL CHECK (amount > 0),
    revenue_multiplier_ppm bigint NOT NULL CHECK (revenue_multiplier_ppm >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    allocation_id text PRIMARY KEY CHECK (allocation_id <> ''),
    request_id text NOT NULL CHECK (request_id <> ''),
    lot_id text NOT NULL REFERENCES v3_billing.funding_lots(lot_id),
    source_account_id text NOT NULL CHECK (source_account_id <> ''),
    source text NOT NULL CHECK (source IN ('topup','blind_box','subscription','legacy_unattributed','other')),
    UNIQUE(request_id,lot_id)
);
CREATE INDEX funding_allocations_request_idx ON v3_billing.funding_allocations(request_id,account_id);

CREATE TABLE v3_billing.request_economics (
    channel_id bigint NOT NULL CHECK (channel_id >= 0),
    route_pool_id bigint NOT NULL CHECK (route_pool_id >= 0),
    actual_amount bigint NOT NULL CHECK (actual_amount >= 0),
    subscription_id bigint NOT NULL CHECK (subscription_id >= 0),
    procurement_cost_multiplier_ppm bigint NOT NULL CHECK (procurement_cost_multiplier_ppm >= 0),
    revenue_multiplier_ppm bigint NOT NULL CHECK (revenue_multiplier_ppm >= 0),
    settled_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    request_id text PRIMARY KEY CHECK (request_id <> ''),
    billing_source text NOT NULL CHECK (billing_source IN ('wallet','subscription','mixed'))
);
CREATE INDEX request_economics_settled_idx ON v3_billing.request_economics(settled_at);

CREATE TABLE v3_billing.wallet_reward_holds (
    account_id bigint NOT NULL REFERENCES v3_billing.accounts(id),
    user_id bigint NOT NULL REFERENCES v3_identity.users(id),
    original_amount bigint NOT NULL CHECK (original_amount > 0),
    consumed_amount bigint NOT NULL DEFAULT 0 CHECK (consumed_amount >= 0 AND consumed_amount <= original_amount),
    user_created_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    hold_id text PRIMARY KEY CHECK (hold_id <> ''),
    source_account_id text NOT NULL CHECK (source_account_id <> ''),
    reference_type text NOT NULL DEFAULT '',
    reference_id text NOT NULL DEFAULT '',
    idempotency_key text NOT NULL UNIQUE CHECK (idempotency_key <> '')
);
CREATE INDEX wallet_reward_holds_fifo_idx ON v3_billing.wallet_reward_holds(account_id,created_at,hold_id) WHERE consumed_amount < original_amount;
