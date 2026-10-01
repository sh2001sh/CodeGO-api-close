CREATE TABLE v3_commerce.group_checkouts (
    order_id bigint PRIMARY KEY REFERENCES v3_commerce.orders(id),
    user_id bigint NOT NULL REFERENCES v3_identity.users(id),
    purchase_type text NOT NULL CHECK(purchase_type IN('group_buy','join_group')),
    requested_group_id bigint CHECK(requested_group_id>0),
    selected_group_id bigint CHECK(selected_group_id>0),
    state text NOT NULL DEFAULT 'pending' CHECK(state IN('pending','applied','review')),
    review_reason text NOT NULL DEFAULT '',
    applied_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK((purchase_type='join_group')=(requested_group_id IS NOT NULL)),
    CHECK((state='applied')=(applied_at IS NOT NULL)),
    CHECK(state<>'applied' OR selected_group_id IS NOT NULL),
    CHECK(state<>'review' OR review_reason<>'')
);

-- The active source checkout creates five-member rooms with a 48-hour window.
-- Explicit native plan values remain available; omitted source values use these.
ALTER TABLE v3_commerce.plans
    ALTER COLUMN group_buy_target SET DEFAULT 5,
    ALTER COLUMN group_buy_lifetime_seconds SET DEFAULT 172800;
ALTER TABLE v3_commerce.orders
    ALTER COLUMN group_buy_target SET DEFAULT 5,
    ALTER COLUMN group_buy_lifetime_seconds SET DEFAULT 172800;
