-- New owner-spendable rewards are permanently distinct from paid principal.
-- Existing funding evidence and blind-box age-based holds keep their semantics.
ALTER TABLE v3_billing.funding_source_policies DROP CONSTRAINT funding_source_policies_source_check;
ALTER TABLE v3_billing.funding_source_policies ADD CONSTRAINT funding_source_policies_source_check
    CHECK (source IN ('topup','blind_box','subscription','legacy_unattributed','other','referral_reward','subscription_conversion'));
ALTER TABLE v3_billing.funding_lots DROP CONSTRAINT funding_lots_source_check;
ALTER TABLE v3_billing.funding_lots ADD CONSTRAINT funding_lots_source_check
    CHECK (source IN ('topup','blind_box','subscription','legacy_unattributed','other','referral_reward','subscription_conversion'));
ALTER TABLE v3_billing.funding_allocations DROP CONSTRAINT funding_allocations_source_check;
ALTER TABLE v3_billing.funding_allocations ADD CONSTRAINT funding_allocations_source_check
    CHECK (source IN ('topup','blind_box','subscription','legacy_unattributed','other','referral_reward','subscription_conversion'));

ALTER TABLE v3_billing.funding_lots
    ADD COLUMN metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    ADD COLUMN non_transferable boolean NOT NULL DEFAULT false,
    ADD COLUMN non_refundable boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT funding_lots_referral_reward_policy_check
        CHECK (source <> 'referral_reward' OR (revenue_multiplier_ppm = 0 AND non_transferable AND non_refundable)),
    ADD CONSTRAINT funding_lots_conversion_policy_check CHECK (
        source <> 'subscription_conversion' OR (
            metadata ?& ARRAY['source','subscription_id','original_order_id','paid_principal_credits','reward_credits']
            AND metadata->>'source' = 'subscription_conversion'
            AND (metadata->>'subscription_id')::numeric > 0
            AND (metadata->>'original_order_id')::numeric >= 0
            AND (
                ((metadata->>'paid_principal_credits')::numeric = original_amount
                 AND (metadata->>'reward_credits')::numeric = 0
                 AND (metadata->>'original_order_id')::numeric > 0
                 AND metadata ? 'revenue_multiplier_ppm'
                 AND (metadata->>'revenue_multiplier_ppm')::numeric = revenue_multiplier_ppm)
                OR
                ((metadata->>'reward_credits')::numeric = original_amount
                 AND (metadata->>'paid_principal_credits')::numeric = 0
                 AND revenue_multiplier_ppm = 0 AND non_transferable AND non_refundable)
            )
        )
    );
ALTER TABLE v3_billing.funding_allocations
    ADD CONSTRAINT funding_allocations_referral_reward_policy_check
        CHECK (source <> 'referral_reward' OR revenue_multiplier_ppm = 0);
ALTER TABLE v3_billing.funding_source_policies
    ADD CONSTRAINT funding_source_policies_referral_reward_policy_check
        CHECK (source <> 'referral_reward' OR revenue_multiplier_ppm = 0);

INSERT INTO v3_billing.funding_source_policies (source, revenue_multiplier_ppm)
    VALUES ('referral_reward', 0), ('subscription_conversion', 0);

-- One immutable settlement fact for each actual funding bucket. NULL monetary
-- factors are unknown and must never be treated as zero-cost paid consumption.
CREATE TABLE v3_billing.funding_source_usage (
    request_id text NOT NULL CHECK (request_id <> ''),
    account_id bigint NOT NULL REFERENCES v3_billing.accounts(id),
    subscription_id bigint CHECK (subscription_id > 0),
    order_id bigint CHECK (order_id > 0),
    policy_version text NOT NULL CHECK (policy_version IN ('legacy','standard_v2','wallet')),
    amount bigint NOT NULL CHECK (amount >= 0),
    wallet_equivalent_amount bigint NOT NULL CHECK (wallet_equivalent_amount >= 0),
    revenue_multiplier_ppm bigint CHECK (revenue_multiplier_ppm >= 0),
    procurement_cost_multiplier_ppm bigint CHECK (procurement_cost_multiplier_ppm >= 0),
    procurement_cost_amount bigint CHECK (procurement_cost_amount >= 0),
    settled_at timestamptz NOT NULL,
    PRIMARY KEY (request_id, account_id)
);
CREATE INDEX funding_source_usage_order_idx ON v3_billing.funding_source_usage(order_id, settled_at)
    WHERE order_id IS NOT NULL;
CREATE INDEX funding_source_usage_settled_idx ON v3_billing.funding_source_usage(settled_at);

-- Also records fully consumed origins: zero-amount ledger entries are forbidden.
-- One original conversion can be revoked once even across provider callbacks.
CREATE TABLE v3_billing.subscription_conversion_revocations (
    operation_id text PRIMARY KEY CHECK (operation_id <> ''),
    subscription_id bigint NOT NULL CHECK (subscription_id > 0),
    original_order_id bigint NOT NULL CHECK (original_order_id > 0),
    revoked_credits bigint NOT NULL CHECK (revoked_credits >= 0),
    consumed_credits bigint NOT NULL CHECK (consumed_credits >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (subscription_id, original_order_id)
);
CREATE INDEX funding_lots_conversion_origin_idx ON v3_billing.funding_lots
    ((metadata->>'subscription_id'), (metadata->>'original_order_id'), account_id)
    WHERE source = 'subscription_conversion';
