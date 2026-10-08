-- Historical async work is readable without inventing an API key, credential
-- or reservation. These rows are never eligible for native reconciliation.
CREATE TABLE v3_workflow.legacy_tasks (
    id text PRIMARY KEY CHECK (id <> ''),
    source_id bigint NOT NULL UNIQUE CHECK (source_id > 0),
    user_id bigint NOT NULL REFERENCES v3_identity.users(id),
    channel_id bigint NOT NULL CHECK (channel_id >= 0),
    group_name text NOT NULL,
    provider text NOT NULL,
    model text NOT NULL,
    upstream_model text NOT NULL,
    upstream_id text NOT NULL,
    action text NOT NULL,
    status text NOT NULL CHECK (status IN ('completed', 'failed')),
    provider_data jsonb NOT NULL,
    result_url text NOT NULL,
    error_message text NOT NULL,
    progress text NOT NULL,
    actual_credits bigint NOT NULL CHECK (actual_credits >= 0),
    workflow_history jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(workflow_history) = 'array'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX legacy_tasks_owner ON v3_workflow.legacy_tasks(user_id, created_at DESC);
