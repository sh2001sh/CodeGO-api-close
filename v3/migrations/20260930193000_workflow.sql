CREATE SCHEMA IF NOT EXISTS v3_workflow;

CREATE TABLE v3_workflow.tasks (
    id text PRIMARY KEY,
    user_id bigint NOT NULL CHECK (user_id > 0),
    key_id bigint NOT NULL CHECK (key_id > 0),
    group_name text NOT NULL,
    target_group text NOT NULL DEFAULT '',
    provider text NOT NULL,
    channel_id bigint NOT NULL CHECK (channel_id > 0),
    credential_id bigint NOT NULL CHECK (credential_id > 0),
    model text NOT NULL,
    upstream_model text NOT NULL,
    upstream_id text NOT NULL DEFAULT '',
    action text NOT NULL,
    status text NOT NULL CHECK (status IN ('submitting','submission_unknown','queued','in_progress','completed','failed')),
    request_body bytea NOT NULL,
    pricing_headers jsonb NOT NULL DEFAULT '{}',
    reservation jsonb NOT NULL,
    provider_data jsonb NOT NULL DEFAULT '{}',
    result_url text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    usage jsonb NOT NULL DEFAULT '{}',
    units double precision NOT NULL DEFAULT 0 CHECK (units >= 0),
    cost_state text NOT NULL DEFAULT 'reserved' CHECK (cost_state IN ('reserved','settled','refunded')),
    actual_credits bigint NOT NULL DEFAULT 0 CHECK (actual_credits >= 0),
    lease_id text NOT NULL DEFAULT '',
    lease_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (status IN ('submitting','submission_unknown','failed') OR upstream_id <> ''),
    CHECK (cost_state <> 'settled' OR status = 'completed'),
    CHECK (cost_state <> 'refunded' OR status = 'failed')
);

CREATE INDEX tasks_owner ON v3_workflow.tasks(user_id, created_at DESC);
CREATE INDEX tasks_reconcile ON v3_workflow.tasks(updated_at)
    WHERE cost_state = 'reserved' AND status IN ('queued','in_progress','completed','failed');
