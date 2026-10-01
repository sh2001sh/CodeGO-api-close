-- Revocable browser sessions; only token digests are stored.
CREATE TABLE v3_identity.sessions (
    id text PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES v3_identity.users(id) ON DELETE CASCADE,
    refresh_hash bytea NOT NULL UNIQUE CHECK (octet_length(refresh_hash) = 32),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);
CREATE INDEX sessions_user_idx ON v3_identity.sessions(user_id);
CREATE INDEX sessions_expiry_idx ON v3_identity.sessions(expires_at);

CREATE TABLE v3_identity.oauth_challenges (
    state_hash bytea PRIMARY KEY CHECK (octet_length(state_hash) = 32),
    provider text NOT NULL,
    verifier text NOT NULL,
    user_id bigint REFERENCES v3_identity.users(id),
    expires_at timestamptz NOT NULL
);

CREATE TABLE v3_identity.passkeys (
    credential_id bytea PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES v3_identity.users(id) ON DELETE CASCADE,
    credential jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);
CREATE INDEX passkeys_user_idx ON v3_identity.passkeys(user_id);
CREATE TABLE v3_identity.passkey_ceremonies (
    state_hash bytea PRIMARY KEY CHECK (octet_length(state_hash)=32),
    purpose text NOT NULL CHECK (purpose IN ('registration','login','verification')),
    user_id bigint REFERENCES v3_identity.users(id),
    session_data jsonb NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE TABLE v3_identity.passkey_verifications (
    proof_hash bytea PRIMARY KEY CHECK (octet_length(proof_hash)=32),
    user_id bigint NOT NULL REFERENCES v3_identity.users(id),
    session_id text NOT NULL REFERENCES v3_identity.sessions(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);
