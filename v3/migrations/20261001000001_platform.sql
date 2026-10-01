-- Shared platform objects: schemas, helpers, snapshot versions and the
-- trigger-fed cache invalidation outbox (plan §3, borrowed from sub2api).

CREATE SCHEMA IF NOT EXISTS v3_platform;
CREATE SCHEMA IF NOT EXISTS v3_identity;
CREATE SCHEMA IF NOT EXISTS v3_catalog;
CREATE SCHEMA IF NOT EXISTS v3_billing;

CREATE FUNCTION v3_platform.touch_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

-- One row per snapshot kind; control publishes a new version, gateways load it
-- in the background and swap an atomic pointer.
CREATE TABLE v3_platform.snapshot_versions (
    kind         text        PRIMARY KEY CHECK (kind IN ('catalog', 'settings', 'pricing')),
    version      bigint      NOT NULL CHECK (version > 0),
    published_at timestamptz NOT NULL DEFAULT now()
);

-- Written by triggers on identity/catalog tables so no admin code path can
-- forget to invalidate. A worker leases rows, publishes over Redis pub/sub,
-- then deletes them.
CREATE TABLE v3_platform.cache_invalidation_outbox (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    entity       text        NOT NULL,
    entity_id    text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    leased_until timestamptz,
    attempts     integer     NOT NULL DEFAULT 0
);

CREATE INDEX cache_invalidation_outbox_pending_idx
    ON v3_platform.cache_invalidation_outbox (id)
    WHERE leased_until IS NULL;

-- Generic trigger: TG_ARGV[0] is the entity name, TG_ARGV[1] the id column.
CREATE FUNCTION v3_platform.enqueue_invalidation() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    row_data jsonb;
BEGIN
    IF TG_OP = 'DELETE' THEN
        row_data := to_jsonb(OLD);
    ELSE
        row_data := to_jsonb(NEW);
    END IF;
    INSERT INTO v3_platform.cache_invalidation_outbox (entity, entity_id)
    VALUES (TG_ARGV[0], row_data ->> TG_ARGV[1]);
    RETURN NULL;
END;
$$;
