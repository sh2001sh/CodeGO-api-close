ALTER TABLE v3_commerce.plans ADD COLUMN IF NOT EXISTS membership_tier text NOT NULL DEFAULT '';

-- Separate receipts keep reward replay safe when it merges into a paid package.
CREATE TABLE v3_commerce.subscription_reward_receipts (
    operation_id text PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES v3_identity.users(id),
    plan_id bigint NOT NULL REFERENCES v3_commerce.plans(id),
    subscription_id bigint NOT NULL REFERENCES v3_commerce.subscriptions(id),
    credits bigint NOT NULL CHECK(credits>=0),
    monthly_seconds bigint NOT NULL CHECK(monthly_seconds>=0),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- A checkout freezes benefit rules before the price or plan can change.
CREATE TABLE v3_commerce.monthly_purchase_benefits (
    order_id bigint PRIMARY KEY REFERENCES v3_commerce.orders(id),
    target_seconds bigint NOT NULL CHECK(target_seconds>=0),
    source_seconds bigint NOT NULL CHECK(source_seconds>=0),
    full_price_minor bigint NOT NULL CHECK(full_price_minor>0)
);
