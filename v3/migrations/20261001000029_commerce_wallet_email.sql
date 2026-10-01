-- A single current purpose-bound code per user; retries and attempts share PG
-- locks. Random code plaintext is sent once and is never persisted.
CREATE TABLE v3_commerce.wallet_recovery_codes (
    user_id bigint PRIMARY KEY REFERENCES v3_identity.users(id),
    failed_attempts integer NOT NULL DEFAULT 0 CHECK(failed_attempts BETWEEN 0 AND 5),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    send_after timestamptz NOT NULL,
    locked_until timestamptz,
    consumed_at timestamptz,
    purpose text NOT NULL CHECK(purpose='wallet_transfer_password'),
    recipient_email text NOT NULL,
    code_hash bytea NOT NULL CHECK(octet_length(code_hash)=32),
    salt bytea NOT NULL CHECK(octet_length(salt)=32),
    state text NOT NULL CHECK(state IN('pending','active','failed','consumed','locked','expired')),
    CHECK(expires_at>created_at)
);
