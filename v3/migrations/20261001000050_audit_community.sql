CREATE SCHEMA IF NOT EXISTS v3_audit;
CREATE SCHEMA IF NOT EXISTS v3_community;

CREATE UNIQUE INDEX channels_community_public_id_idx ON v3_catalog.channels
    ((settings->'community'->>'id'))
    WHERE scope='marketplace' AND COALESCE(settings->'community'->>'id','')<>'';

CREATE TABLE v3_audit.request_samples (
    request_id text PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES v3_identity.users(id),
    model text NOT NULL,
    created_at timestamptz NOT NULL,
    request_body jsonb NOT NULL,
    response_body jsonb NOT NULL
);
CREATE INDEX request_samples_created_idx ON v3_audit.request_samples (created_at);

-- Preserve the public channel identifier used by NodeBB, independently of the
-- numeric gateway channel identifier. Marketplace is the ownership authority.
CREATE TABLE v3_community.channel_ratings (
    channel_id text NOT NULL,
    user_id bigint NOT NULL REFERENCES v3_identity.users(id),
    stars smallint NOT NULL CHECK (stars BETWEEN 1 AND 5),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, user_id)
);
CREATE INDEX channel_ratings_user_idx ON v3_community.channel_ratings (user_id);
CREATE TRIGGER channel_ratings_touch BEFORE UPDATE ON v3_community.channel_ratings
    FOR EACH ROW EXECUTE FUNCTION v3_platform.touch_updated_at();
