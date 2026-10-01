CREATE SCHEMA v3_security;

CREATE TABLE v3_security.account_request_abuse_states (
    user_id bigint PRIMARY KEY,
    strikes integer NOT NULL,
    restricted_until bigint NOT NULL,
    last_window_end bigint NOT NULL,
    blocked boolean NOT NULL,
    evidence text,
    updated_at bigint
);

CREATE TABLE v3_security.security_audit_events (
    id varchar(64) PRIMARY KEY,
    dedupe_key varchar(64) UNIQUE NOT NULL,
    request_id varchar(128), source varchar(32) NOT NULL,
    decision varchar(24) NOT NULL, risk_code varchar(64) NOT NULL, severity varchar(16) NOT NULL,
    user_id bigint, token_id bigint, token_name varchar(128), channel_id bigint,
    marketplace_channel_id varchar(64), marketplace_group_id varchar(64), owner_user_id bigint,
    model varchar(191), protocol varchar(96), http_status integer,
    upstream_error_type varchar(64), upstream_error_code varchar(64),
    upstream_error_message text, upstream_error_body text,
    prompt_hash varchar(64), prompt_preview varchar(512), prompt_length integer, message_count integer,
    billing_result varchar(24), notification_status varchar(24),
    notification_targets integer NOT NULL DEFAULT 0, notification_success integer NOT NULL DEFAULT 0,
    notified_at timestamptz, review_status varchar(24) NOT NULL,
    review_note varchar(1000), reviewed_by bigint, reviewed_at timestamptz,
    created_at timestamptz, updated_at timestamptz
);
CREATE INDEX security_audit_owner_idx ON v3_security.security_audit_events(owner_user_id,created_at DESC,id);
CREATE INDEX security_audit_created_idx ON v3_security.security_audit_events(created_at DESC,id);

-- These bounded counters are updated only by fresh, completed ledger usage.
CREATE TABLE v3_security.usage_samples (
    user_id bigint NOT NULL, model_hash text NOT NULL, minute bigint NOT NULL,
    requests bigint NOT NULL, short_requests bigint NOT NULL,
    input_tokens bigint NOT NULL, cached_tokens bigint NOT NULL, unknown_requests bigint NOT NULL,
    PRIMARY KEY(user_id,model_hash,minute)
);
CREATE TABLE v3_security.cache_support (
    channel_id bigint NOT NULL, model_hash text NOT NULL, expires_at bigint NOT NULL,
    PRIMARY KEY(channel_id,model_hash)
);
CREATE TABLE v3_security.state_cache_refresh (user_id bigint PRIMARY KEY);

-- A fresh ledger event commits only this queue row. Sampling never takes
-- identity locks while financial/subscription/marketplace locks are held.
CREATE TABLE v3_security.usage_queue (
    request_id text PRIMARY KEY, user_id bigint NOT NULL, channel_id bigint NOT NULL,
    model text NOT NULL, input_tokens bigint NOT NULL, cached_tokens bigint NOT NULL,
    observed_at bigint NOT NULL
);
CREATE INDEX security_usage_queue_time_idx ON v3_security.usage_queue(observed_at,request_id);
