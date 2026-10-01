-- Refund requests reserve the unused benefit before contacting the provider.
CREATE TABLE v3_commerce.user_refunds (
    refund_no text PRIMARY KEY,
    order_id bigint NOT NULL REFERENCES v3_commerce.orders(id),
    user_id bigint NOT NULL REFERENCES v3_identity.users(id),
    account_id bigint NOT NULL REFERENCES v3_billing.accounts(id),
    subscription_id bigint REFERENCES v3_commerce.subscriptions(id),
    original_subscription_state text NOT NULL DEFAULT '',
    provider_order_id text NOT NULL CHECK (provider_order_id <> ''),
    provider_refund_id text NOT NULL DEFAULT '',
    gross_minor bigint NOT NULL CHECK (gross_minor > 0),
    fee_minor bigint NOT NULL CHECK (fee_minor >= 0 AND fee_minor <= gross_minor),
    amount_minor bigint NOT NULL CHECK (amount_minor > 0 AND amount_minor = gross_minor - fee_minor),
    refund_credits bigint NOT NULL CHECK (refund_credits > 0),
    reserved_credits bigint NOT NULL CHECK (reserved_credits >= 0),
    status text NOT NULL CHECK (status IN ('processing','success','failed')),
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX user_refunds_active_order_idx ON v3_commerce.user_refunds(order_id) WHERE status <> 'failed';
CREATE INDEX user_refunds_user_idx ON v3_commerce.user_refunds(user_id,created_at DESC);
CREATE INDEX user_refunds_pending_idx ON v3_commerce.user_refunds(updated_at) WHERE status='processing';

-- A crash cannot strand a wallet's Redis admission flag. Recovery reopens only
-- the exact token owned by this table, never a retired subscription bucket.
CREATE TABLE v3_commerce.user_refund_freezes (
    account_id bigint PRIMARY KEY REFERENCES v3_billing.accounts(id),
    token text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL
);

-- The offline importer supplies verifiable remaining v2 funding lots against
-- the wallet's opening-entry watermark. Without these, old orders fail closed.
CREATE TABLE v3_commerce.user_refund_origins (
    order_id bigint PRIMARY KEY REFERENCES v3_commerce.orders(id),
    account_id bigint NOT NULL REFERENCES v3_billing.accounts(id),
    original_credits bigint NOT NULL CHECK (original_credits > 0),
    remaining_credits bigint NOT NULL CHECK (remaining_credits BETWEEN 0 AND original_credits),
    ledger_cursor bigint NOT NULL REFERENCES v3_billing.ledger_entries(id)
);
CREATE INDEX user_refund_origins_account_idx ON v3_commerce.user_refund_origins(account_id);

-- V2 referral resets can erase projected amount_used. The importer must verify
-- cumulative consumed minus refunded against its billing snapshot; an opening
-- subscription bucket without this provenance cannot be refunded.
CREATE TABLE v3_commerce.user_refund_subscription_origins (
    subscription_id bigint PRIMARY KEY REFERENCES v3_commerce.subscriptions(id),
    order_id bigint NOT NULL UNIQUE REFERENCES v3_commerce.orders(id),
    used_credits bigint NOT NULL CHECK (used_credits >= 0)
);
