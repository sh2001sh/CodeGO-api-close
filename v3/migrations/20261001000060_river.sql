-- River v0.47.0 main migration 007 schema, maintained by Atlas alongside the
-- application schema. River runtime must not migrate production databases.
CREATE TYPE v3_platform.river_job_state AS ENUM
    ('available', 'cancelled', 'completed', 'discarded', 'pending', 'retryable', 'running', 'scheduled');

CREATE TABLE v3_platform.river_job (
    id bigserial PRIMARY KEY,
    state v3_platform.river_job_state NOT NULL DEFAULT 'available',
    attempt smallint NOT NULL DEFAULT 0,
    max_attempts smallint NOT NULL DEFAULT 25 CHECK (max_attempts > 0),
    attempted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    finalized_at timestamptz,
    scheduled_at timestamptz NOT NULL DEFAULT now(),
    priority smallint NOT NULL DEFAULT 1 CHECK (priority BETWEEN 1 AND 4),
    args jsonb NOT NULL,
    attempted_by text[],
    errors jsonb[],
    kind text NOT NULL CHECK (char_length(kind) > 0 AND char_length(kind) < 128),
    metadata jsonb NOT NULL DEFAULT '{}',
    queue text NOT NULL DEFAULT 'default' CHECK (char_length(queue) > 0 AND char_length(queue) < 128),
    tags varchar(255)[] NOT NULL DEFAULT '{}',
    unique_key bytea,
    unique_states bit(8),
    CONSTRAINT finalized_or_finalized_at_null CHECK (
        (finalized_at IS NULL AND state NOT IN ('cancelled', 'completed', 'discarded')) OR
        (finalized_at IS NOT NULL AND state IN ('cancelled', 'completed', 'discarded')))
);
CREATE INDEX river_job_kind ON v3_platform.river_job (kind);
CREATE INDEX river_job_state_and_finalized_at_index ON v3_platform.river_job (state, finalized_at) WHERE finalized_at IS NOT NULL;
CREATE INDEX river_job_prioritized_fetching_index ON v3_platform.river_job (state, queue, priority, scheduled_at, id);
CREATE INDEX river_job_args_index ON v3_platform.river_job USING gin (args);
CREATE INDEX river_job_metadata_index ON v3_platform.river_job USING gin (metadata);

CREATE FUNCTION v3_platform.river_job_state_in_bitmask(bitmask bit(8), state v3_platform.river_job_state)
RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE state
        WHEN 'available' THEN get_bit(bitmask, 7)
        WHEN 'cancelled' THEN get_bit(bitmask, 6)
        WHEN 'completed' THEN get_bit(bitmask, 5)
        WHEN 'discarded' THEN get_bit(bitmask, 4)
        WHEN 'pending' THEN get_bit(bitmask, 3)
        WHEN 'retryable' THEN get_bit(bitmask, 2)
        WHEN 'running' THEN get_bit(bitmask, 1)
        WHEN 'scheduled' THEN get_bit(bitmask, 0)
        ELSE 0 END = 1;
$$;
CREATE UNIQUE INDEX river_job_unique_idx ON v3_platform.river_job (unique_key)
    WHERE unique_key IS NOT NULL AND unique_states IS NOT NULL
      AND v3_platform.river_job_state_in_bitmask(unique_states, state);

CREATE UNLOGGED TABLE v3_platform.river_leader (
    elected_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    leader_id text NOT NULL CHECK (char_length(leader_id) > 0 AND char_length(leader_id) < 128),
    name text PRIMARY KEY DEFAULT 'default' CHECK (name = 'default')
);
CREATE TABLE v3_platform.river_queue (
    name text PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    metadata jsonb NOT NULL DEFAULT '{}',
    paused_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE v3_platform.river_notification (
    id bigserial PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    payload text NOT NULL,
    topic text NOT NULL CHECK (length(topic) > 0 AND length(topic) < 128)
);
CREATE INDEX river_notification_created_at_idx ON v3_platform.river_notification (created_at);
CREATE INDEX river_notification_topic_id_idx ON v3_platform.river_notification (topic, id);
