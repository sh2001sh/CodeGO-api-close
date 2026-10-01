-- Confidential-client grants and access tokens contain digests, never secrets.
-- Email verification is an operator-controlled fact, independent of self-editable settings.
ALTER TABLE v3_identity.users ADD COLUMN email_verified boolean NOT NULL DEFAULT false;

CREATE TABLE v3_identity.oidc_codes (
    code_hash bytea PRIMARY KEY CHECK (octet_length(code_hash) = 32),
    client_id text NOT NULL,
    user_id bigint NOT NULL REFERENCES v3_identity.users(id) ON DELETE CASCADE,
    redirect_uri text NOT NULL,
    scope text NOT NULL,
    nonce text NOT NULL DEFAULT '',
    code_challenge text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX oidc_codes_expiry_idx ON v3_identity.oidc_codes(expires_at);
CREATE TABLE v3_identity.oidc_tokens (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    client_id text NOT NULL,
    user_id bigint NOT NULL REFERENCES v3_identity.users(id) ON DELETE CASCADE,
    scope text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);
CREATE INDEX oidc_tokens_expiry_idx ON v3_identity.oidc_tokens(expires_at);
CREATE INDEX oidc_tokens_user_idx ON v3_identity.oidc_tokens(user_id);
