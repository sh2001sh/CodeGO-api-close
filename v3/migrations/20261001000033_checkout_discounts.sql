-- Frozen checkout pricing and reservation lifecycle share the order transaction.
CREATE TABLE v3_commerce.checkout_discounts (
    order_id bigint PRIMARY KEY REFERENCES v3_commerce.orders(id),
    original_minor bigint NOT NULL CHECK(original_minor>0),
    original_known boolean NOT NULL DEFAULT true,
    paid_minor bigint NOT NULL CHECK(paid_minor>0 AND paid_minor<=original_minor),
    campaign boolean NOT NULL,
    multiplier text NOT NULL,
    starts_at bigint NOT NULL DEFAULT 0,
    ends_at bigint NOT NULL DEFAULT 0,
    prop_id bigint CHECK(prop_id>0),
    state text NOT NULL DEFAULT 'reserved' CHECK(state IN('reserved','consumed','released','review')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK((campaign AND prop_id IS NULL) OR (NOT campaign AND prop_id IS NOT NULL))
);
CREATE INDEX checkout_discounts_pending ON v3_commerce.checkout_discounts(order_id) WHERE state='reserved';
