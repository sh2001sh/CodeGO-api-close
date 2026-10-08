-- New finite campaigns never rewrite historical pools, inventory or pity.
ALTER TABLE v3_billing.accounts DROP CONSTRAINT accounts_kind_check;
ALTER TABLE v3_billing.accounts ADD CONSTRAINT accounts_kind_check CHECK(kind IN('wallet','key_budget','subscription','marketplace_pending','marketplace_earned','platform_revenue','affiliate','promotion_budget'));
CREATE TABLE v3_marketplace.blind_box_batches (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 price_micro bigint NOT NULL CHECK(price_micro>=0),
 base_credits_micro bigint NOT NULL CHECK(base_credits_micro>=0),
 budget_micro bigint NOT NULL CHECK(budget_micro>=0),
 required_budget_micro bigint NOT NULL DEFAULT 0 CHECK(required_budget_micro>=0),
 spent_budget_micro bigint NOT NULL DEFAULT 0 CHECK(spent_budget_micro>=0),
 escrow_account_id bigint REFERENCES v3_billing.accounts(id),
 total_count bigint NOT NULL DEFAULT 0 CHECK(total_count>=0),
 remaining_count bigint NOT NULL DEFAULT 0 CHECK(remaining_count>=0 AND remaining_count<=total_count),
 entitled_count bigint NOT NULL DEFAULT 0 CHECK(entitled_count>=0 AND entitled_count<=remaining_count),
 ancillary_cost_ppm bigint NOT NULL DEFAULT 30000 CHECK(ancillary_cost_ppm BETWEEN 0 AND 1000000),
 contribution_share_ppm bigint NOT NULL DEFAULT 100000 CHECK(contribution_share_ppm BETWEEN 1 AND 100000),
 costs_confirmed boolean NOT NULL DEFAULT false,
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 200),
 purpose text NOT NULL CHECK(purpose IN('consumption','credits')),
 state text NOT NULL DEFAULT 'draft' CHECK(state IN('draft','published','paused','exhausted')),
 rewards jsonb NOT NULL CHECK(jsonb_typeof(rewards)='array' AND jsonb_array_length(rewards)>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 published_at timestamptz,
 CHECK(spent_budget_micro<=required_budget_micro AND required_budget_micro<=budget_micro),
 CHECK((purpose='consumption' AND price_micro=0 AND base_credits_micro=0) OR (purpose='credits' AND price_micro>0 AND base_credits_micro>=price_micro)),
 CHECK(state='draft' OR (escrow_account_id IS NOT NULL AND published_at IS NOT NULL AND total_count>0))
);
CREATE INDEX blind_box_batches_visible_idx ON v3_marketplace.blind_box_batches(state,id DESC);
CREATE TABLE v3_marketplace.blind_box_batch_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 batch_id bigint NOT NULL REFERENCES v3_marketplace.blind_box_batches(id),
 actor_id bigint NOT NULL REFERENCES v3_identity.users(id),
 revision bigint NOT NULL CHECK(revision>0),
 action text NOT NULL CHECK(action IN('publish','pause')),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE v3_marketplace.blind_box_batch_entitlements (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 batch_id bigint NOT NULL REFERENCES v3_marketplace.blind_box_batches(id),
 user_id bigint NOT NULL REFERENCES v3_identity.users(id),
 available_count bigint NOT NULL DEFAULT 0 CHECK(available_count>=0),
 awarded_count bigint NOT NULL DEFAULT 0 CHECK(awarded_count>=available_count),
 contribution_micro bigint NOT NULL DEFAULT 0 CHECK(contribution_micro>=0),
 committed_budget_micro bigint NOT NULL DEFAULT 0 CHECK(committed_budget_micro>=0),
 source text NOT NULL DEFAULT 'settled_paid_consumption' CHECK(source='settled_paid_consumption'),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(batch_id,user_id)
);
CREATE TABLE v3_marketplace.blind_box_contribution_claims (
 request_id text NOT NULL,
 account_id bigint NOT NULL REFERENCES v3_billing.accounts(id),
 batch_id bigint NOT NULL REFERENCES v3_marketplace.blind_box_batches(id),
 user_id bigint NOT NULL REFERENCES v3_identity.users(id),
 contribution_micro bigint NOT NULL CHECK(contribution_micro>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(request_id,account_id)
);
ALTER TABLE v3_marketplace.blind_box_open_records ADD COLUMN batch_id bigint REFERENCES v3_marketplace.blind_box_batches(id);
ALTER TABLE v3_marketplace.blind_box_props ADD COLUMN plan_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb CHECK(jsonb_typeof(plan_snapshot)='object');

ALTER TABLE v3_billing.funding_source_policies DROP CONSTRAINT funding_source_policies_source_check;
ALTER TABLE v3_billing.funding_lots DROP CONSTRAINT funding_lots_source_check;
ALTER TABLE v3_billing.funding_allocations DROP CONSTRAINT funding_allocations_source_check;
ALTER TABLE v3_billing.funding_source_policies ADD CONSTRAINT funding_source_policies_source_check CHECK(source IN('topup','blind_box','subscription','legacy_unattributed','other','referral_reward','subscription_conversion','blind_box_batch_base','blind_box_batch_reward'));
ALTER TABLE v3_billing.funding_lots ADD CONSTRAINT funding_lots_source_check CHECK(source IN('topup','blind_box','subscription','legacy_unattributed','other','referral_reward','subscription_conversion','blind_box_batch_base','blind_box_batch_reward'));
ALTER TABLE v3_billing.funding_allocations ADD CONSTRAINT funding_allocations_source_check CHECK(source IN('topup','blind_box','subscription','legacy_unattributed','other','referral_reward','subscription_conversion','blind_box_batch_base','blind_box_batch_reward'));
INSERT INTO v3_billing.funding_source_policies(source,revenue_multiplier_ppm) VALUES('blind_box_batch_base',0),('blind_box_batch_reward',0);
ALTER TABLE v3_billing.funding_lots ADD CONSTRAINT funding_lots_blind_batch_check CHECK(source NOT IN('blind_box_batch_base','blind_box_batch_reward') OR (revenue_multiplier_ppm=0 AND non_transferable AND non_refundable));
