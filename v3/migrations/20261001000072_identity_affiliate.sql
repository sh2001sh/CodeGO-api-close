-- Withdrawal receipts preserve monetary idempotency independently of retries.
CREATE TABLE v3_identity.affiliate_transfers (
 user_id bigint NOT NULL REFERENCES v3_identity.users(id),
 operation_id text NOT NULL CHECK(length(operation_id) BETWEEN 1 AND 128),
 amount bigint NOT NULL CHECK(amount >= 1000000),
 affiliate_balance bigint NOT NULL CHECK(affiliate_balance >= 0),
 wallet_balance bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,operation_id)
);
