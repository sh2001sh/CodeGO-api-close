-- Cashier selections are immutable with the order, including a checkout
-- resumed after draining an earlier subscription or restarting the worker.
ALTER TABLE v3_commerce.orders
    ADD COLUMN checkout_selection jsonb NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(checkout_selection) = 'object');
