-- Explicit limits compile into cached principals and immutable route plans.
ALTER TABLE v3_identity.users
    ADD COLUMN max_concurrency integer NOT NULL DEFAULT 0 CHECK (max_concurrency >= 0),
    ADD COLUMN requests_per_minute integer NOT NULL DEFAULT 0 CHECK (requests_per_minute >= 0);

ALTER TABLE v3_catalog.channel_credentials
    ADD COLUMN max_concurrency integer NOT NULL DEFAULT 0 CHECK (max_concurrency >= 0);

DROP TRIGGER users_invalidate ON v3_identity.users;
CREATE TRIGGER users_invalidate
    AFTER INSERT OR DELETE OR UPDATE OF status, role, group_name, deleted_at,
        max_concurrency, requests_per_minute ON v3_identity.users
    FOR EACH ROW EXECUTE FUNCTION v3_platform.enqueue_invalidation('user', 'id');
