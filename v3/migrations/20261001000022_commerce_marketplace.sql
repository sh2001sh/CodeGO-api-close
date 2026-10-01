-- Paid orders freeze group-buy eligibility and bonus rules alongside price.
ALTER TABLE v3_commerce.plans
    ADD COLUMN group_buy_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN group_buy_target integer NOT NULL DEFAULT 3 CHECK(group_buy_target BETWEEN 2 AND 1000),
    ADD COLUMN group_buy_bonus bigint NOT NULL DEFAULT 0 CHECK(group_buy_bonus>=0),
    ADD COLUMN group_buy_lifetime_seconds bigint NOT NULL DEFAULT 86400 CHECK(group_buy_lifetime_seconds BETWEEN 60 AND 31622400);
ALTER TABLE v3_commerce.orders
    ADD COLUMN group_buy_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN group_buy_target integer NOT NULL DEFAULT 3 CHECK(group_buy_target BETWEEN 2 AND 1000),
    ADD COLUMN group_buy_bonus bigint NOT NULL DEFAULT 0 CHECK(group_buy_bonus>=0),
    ADD COLUMN group_buy_lifetime_seconds bigint NOT NULL DEFAULT 86400 CHECK(group_buy_lifetime_seconds BETWEEN 60 AND 31622400);
ALTER TABLE v3_commerce.subscriptions ADD COLUMN reward_operation text UNIQUE;
