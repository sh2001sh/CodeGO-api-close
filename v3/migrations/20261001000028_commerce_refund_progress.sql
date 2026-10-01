-- Provider callbacks report cumulative refunded payment amounts. Retaining the
-- corresponding cumulative reversed credits makes out-of-order partial refunds
-- harmless and permits the final refund to reverse the exact original grant.
CREATE TABLE v3_commerce.provider_refund_progress (
    order_id bigint PRIMARY KEY REFERENCES v3_commerce.orders(id),
    amount_minor bigint NOT NULL CHECK(amount_minor > 0),
    reversed_credits bigint NOT NULL CHECK(reversed_credits >= 0),
    updated_at timestamptz NOT NULL
);
