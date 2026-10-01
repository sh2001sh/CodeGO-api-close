-- Fiat and crypto price currencies use the same integer payment minor units.
ALTER TABLE v3_commerce.plans DROP CONSTRAINT plans_currency_check;
ALTER TABLE v3_commerce.plans ADD CONSTRAINT plans_currency_check CHECK(currency ~ '^[a-z][a-z0-9]{2,11}$');
ALTER TABLE v3_commerce.orders DROP CONSTRAINT orders_currency_check;
ALTER TABLE v3_commerce.orders ADD CONSTRAINT orders_currency_check CHECK(currency ~ '^[a-z][a-z0-9]{2,11}$');
ALTER TABLE v3_commerce.orders ADD COLUMN product_id text NOT NULL DEFAULT '';
