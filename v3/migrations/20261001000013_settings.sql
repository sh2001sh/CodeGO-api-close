-- Runtime configuration is versioned with the catalog. Secrets remain in PG
-- as AEAD ciphertext and never enter gateway snapshots or admin list responses.
CREATE TABLE v3_platform.settings (
    key         text PRIMARY KEY CHECK (key <> '' AND length(key) <= 255),
    value       jsonb,
    ciphertext  bytea,
    sensitive   boolean NOT NULL DEFAULT false,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CHECK ((sensitive AND value IS NULL AND ciphertext IS NOT NULL)
        OR (NOT sensitive AND value IS NOT NULL AND ciphertext IS NULL))
);

CREATE TRIGGER settings_touch BEFORE UPDATE ON v3_platform.settings
    FOR EACH ROW EXECUTE FUNCTION v3_platform.touch_updated_at();
CREATE TRIGGER settings_invalidate AFTER INSERT OR UPDATE OR DELETE ON v3_platform.settings
    FOR EACH ROW EXECUTE FUNCTION v3_platform.enqueue_invalidation('settings', 'key');
