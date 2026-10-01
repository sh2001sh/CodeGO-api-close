-- Imported records preserve historical facts without posting to the live ledger.
CREATE TABLE v3_audit.events (
    id bigint PRIMARY KEY,
    user_id bigint NOT NULL,
    created_at timestamptz NOT NULL,
    event_type int NOT NULL CHECK (event_type BETWEEN 0 AND 6),
    content text NOT NULL,
    username text NOT NULL,
    token_name text NOT NULL,
    model text NOT NULL,
    amount bigint NOT NULL,
    prompt_tokens bigint NOT NULL CHECK (prompt_tokens >= 0),
    completion_tokens bigint NOT NULL CHECK (completion_tokens >= 0),
    duration_seconds bigint NOT NULL CHECK (duration_seconds >= 0),
    is_stream boolean NOT NULL,
    channel_id bigint NOT NULL,
    key_id bigint NOT NULL,
    group_name text NOT NULL,
    ip text NOT NULL,
    request_id text NOT NULL,
    upstream_request_id text NOT NULL,
    metadata jsonb NOT NULL
);
CREATE INDEX audit_events_user_date_idx ON v3_audit.events (user_id, created_at DESC, id DESC);
CREATE INDEX audit_events_key_date_idx ON v3_audit.events (user_id, key_id, created_at DESC, id DESC);

CREATE TABLE v3_audit.request_audits (
    request_id text PRIMARY KEY,
    trace_id text NOT NULL,
    user_id bigint NOT NULL,
    key_id bigint NOT NULL,
    model text NOT NULL,
    group_name text NOT NULL,
    protocol text NOT NULL,
    request_type text NOT NULL,
    status text NOT NULL,
    counted_in_success_rate boolean NOT NULL,
    billable boolean NOT NULL,
    amount bigint NOT NULL CHECK (amount >= 0),
    prompt_tokens bigint NOT NULL CHECK (prompt_tokens >= 0),
    completion_tokens bigint NOT NULL CHECK (completion_tokens >= 0),
    final_channel_id bigint NOT NULL,
    attempts_count bigint NOT NULL CHECK (attempts_count >= 0),
    retry_count bigint NOT NULL CHECK (retry_count >= 0),
    status_code bigint NOT NULL,
    error_code text NOT NULL,
    started_at timestamptz NOT NULL,
    completed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE INDEX request_audits_user_date_idx ON v3_audit.request_audits (user_id, started_at DESC, request_id DESC);
CREATE INDEX request_audits_key_date_idx ON v3_audit.request_audits (user_id, key_id, started_at DESC, request_id DESC);

CREATE TABLE v3_audit.request_attempt_audits (
    attempt_id text PRIMARY KEY,
    request_id text NOT NULL REFERENCES v3_audit.request_audits(request_id),
    attempt_no bigint NOT NULL CHECK (attempt_no >= 0),
    retry_index bigint NOT NULL CHECK (retry_index >= 0),
    channel_id bigint NOT NULL,
    model text NOT NULL,
    fault_domain text NOT NULL,
    request_type text NOT NULL,
    status text NOT NULL,
    success boolean NOT NULL,
    status_code bigint NOT NULL,
    failure_class text NOT NULL,
    stage text NOT NULL,
    started_at timestamptz NOT NULL,
    completed_at timestamptz NOT NULL,
    duration_ms bigint NOT NULL CHECK (duration_ms >= 0),
    created_at timestamptz NOT NULL
);
CREATE INDEX request_attempts_request_idx ON v3_audit.request_attempt_audits (request_id, attempt_no, attempt_id);
