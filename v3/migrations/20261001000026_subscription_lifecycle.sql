-- Period budgets are admission balances. Retired accounts stay closed so a
-- stale gateway profile cannot spend an old cycle after a reset.
ALTER TABLE v3_commerce.plans
    ADD COLUMN IF NOT EXISTS period_credits bigint NOT NULL DEFAULT 0 CHECK (period_credits >= 0),
    ADD COLUMN IF NOT EXISTS reset_period text NOT NULL DEFAULT 'never' CHECK (reset_period IN ('never','daily','weekly','monthly','custom')),
    ADD COLUMN IF NOT EXISTS reset_custom_seconds bigint NOT NULL DEFAULT 0 CHECK (reset_custom_seconds >= 0),
    ADD COLUMN IF NOT EXISTS internal_only boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS max_purchase_per_user integer NOT NULL DEFAULT 0 CHECK (max_purchase_per_user >= 0),
    ADD COLUMN IF NOT EXISTS duration_unit text NOT NULL DEFAULT 'custom',
    ADD COLUMN IF NOT EXISTS duration_value integer NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS custom_seconds bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS plan_type text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS group_buy_bonus2_micro bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS group_buy_bonus3_micro bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS group_buy_bonus5_micro bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS fuel_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS fuel_unit_price_micro bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS fuel_min_credits bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS fuel_credit_step bigint NOT NULL DEFAULT 0;
ALTER TABLE v3_commerce.orders
    ADD COLUMN IF NOT EXISTS period_credits bigint NOT NULL DEFAULT 0 CHECK (period_credits >= 0),
    ADD COLUMN IF NOT EXISTS reset_period text NOT NULL DEFAULT 'never',
    ADD COLUMN IF NOT EXISTS reset_custom_seconds bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS legacy_periodic boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS duration_unit text NOT NULL DEFAULT 'custom',
    ADD COLUMN IF NOT EXISTS duration_value integer NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS custom_seconds bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS fulfillment_state text NOT NULL DEFAULT 'completed',
    ADD COLUMN IF NOT EXISTS group_buy_bonus2_micro bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS group_buy_bonus3_micro bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS group_buy_bonus5_micro bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS purchase_type text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS target_subscription_id bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS fuel_expires_at timestamptz;
ALTER TABLE v3_commerce.plans DROP CONSTRAINT plans_credits_check;
ALTER TABLE v3_commerce.plans ADD CONSTRAINT plans_credits_check CHECK (credits>=0 AND (credits>0 OR period_credits>0));
ALTER TABLE v3_commerce.orders DROP CONSTRAINT orders_credits_check;
ALTER TABLE v3_commerce.orders ADD CONSTRAINT orders_credits_check CHECK (credits>=0 AND (credits>0 OR (kind='subscription' AND period_credits>0)));
ALTER TABLE v3_commerce.subscriptions
    ADD COLUMN IF NOT EXISTS total_credits bigint NOT NULL DEFAULT 0 CHECK (total_credits >= 0),
    ADD COLUMN IF NOT EXISTS renewable_credits bigint NOT NULL DEFAULT 0 CHECK (renewable_credits >= 0),
    ADD COLUMN IF NOT EXISTS used_credits bigint NOT NULL DEFAULT 0 CHECK (used_credits >= 0),
    ADD COLUMN IF NOT EXISTS period_credits bigint NOT NULL DEFAULT 0 CHECK (period_credits >= 0),
    ADD COLUMN IF NOT EXISTS period_used bigint NOT NULL DEFAULT 0 CHECK (period_used >= 0),
    ADD COLUMN IF NOT EXISTS legacy_periodic boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS last_reset_at timestamptz,
    ADD COLUMN IF NOT EXISTS next_reset_at timestamptz,
    ADD COLUMN IF NOT EXISTS reset_period text NOT NULL DEFAULT 'never',
    ADD COLUMN IF NOT EXISTS reset_custom_seconds bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS deleted_at timestamptz,
    ADD COLUMN IF NOT EXISTS reset_opportunity_used boolean NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS subscriptions_reset_idx ON v3_commerce.subscriptions(next_reset_at)
    WHERE state='active' AND next_reset_at IS NOT NULL;
CREATE SEQUENCE v3_commerce.subscription_bucket_ids AS bigint MINVALUE 1;
CREATE TABLE v3_commerce.subscription_buckets (
    account_id bigint PRIMARY KEY REFERENCES v3_billing.accounts(id),
    subscription_id bigint NOT NULL REFERENCES v3_commerce.subscriptions(id),
    starts_at timestamptz NOT NULL,
    ended_at timestamptz
);
CREATE INDEX subscription_buckets_subscription_idx ON v3_commerce.subscription_buckets(subscription_id);
CREATE TABLE v3_commerce.subscription_operations (
    operation_id text PRIMARY KEY,
    subscription_id bigint NOT NULL REFERENCES v3_commerce.subscriptions(id),
    actor_id bigint NOT NULL REFERENCES v3_identity.users(id),
    kind text NOT NULL CHECK (kind IN ('reset','update','renew','upgrade','conversion','invalidate','delete')),
    payload jsonb NOT NULL DEFAULT '{}',
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','completed','failed')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX subscription_operations_pending_idx ON v3_commerce.subscription_operations(created_at) WHERE state='pending';
CREATE TABLE v3_commerce.package_checkouts (
    order_id bigint PRIMARY KEY REFERENCES v3_commerce.orders(id),
    user_id bigint NOT NULL REFERENCES v3_identity.users(id),
    request_id text NOT NULL,
    target_subscription_id bigint NOT NULL REFERENCES v3_commerce.subscriptions(id),
    source_account_id bigint NOT NULL REFERENCES v3_billing.accounts(id),
    action text NOT NULL CHECK (action IN ('auto','renew','upgrade')),
    resolved_action text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'preparing' CHECK (state IN ('preparing','checkout','applied','restored')),
    success_url text NOT NULL,
    cancel_url text NOT NULL,
    quoted_used bigint NOT NULL DEFAULT 0 CHECK (quoted_used>=0),
    quoted_remaining bigint NOT NULL DEFAULT 0 CHECK (quoted_remaining>=0),
    preserve_remaining boolean NOT NULL DEFAULT false,
    bonus_credits bigint NOT NULL DEFAULT 0 CHECK (bonus_credits>=0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(user_id,request_id)
);
CREATE UNIQUE INDEX package_checkouts_active_target_idx ON v3_commerce.package_checkouts(target_subscription_id)
    WHERE state IN ('preparing','checkout');
CREATE TABLE v3_commerce.package_payment_reviews (
    order_id bigint PRIMARY KEY REFERENCES v3_commerce.orders(id),
    user_id bigint NOT NULL REFERENCES v3_identity.users(id),
    reason text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz
);
INSERT INTO v3_commerce.subscription_buckets(account_id,subscription_id,starts_at)
    SELECT account_id,id,starts_at FROM v3_commerce.subscriptions WHERE account_id IS NOT NULL;
UPDATE v3_commerce.subscriptions s SET total_credits=o.credits
    FROM v3_commerce.orders o WHERE o.id=s.order_id AND s.total_credits=0;
UPDATE v3_commerce.subscriptions s SET renewable_credits=LEAST(s.total_credits,p.credits)
    FROM v3_commerce.plans p WHERE p.id=s.plan_id AND s.renewable_credits=0;
CREATE TABLE v3_commerce.subscription_conversions (
    operation_id text PRIMARY KEY REFERENCES v3_commerce.subscription_operations(operation_id),
    user_id bigint NOT NULL REFERENCES v3_identity.users(id),
    subscription_id bigint NOT NULL REFERENCES v3_commerce.subscriptions(id),
    conversion_percent integer NOT NULL,
    cycle_order_id bigint NOT NULL DEFAULT 0,
    source_credits bigint NOT NULL CHECK(source_credits>0),
    target_credits bigint NOT NULL CHECK(target_credits>0),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE v3_commerce.subscription_fuel_fulfillments (
    order_id bigint PRIMARY KEY REFERENCES v3_commerce.orders(id),
    subscription_id bigint NOT NULL REFERENCES v3_commerce.subscriptions(id),
    credits bigint NOT NULL CHECK(credits>0),
    revoked boolean NOT NULL DEFAULT false
);
