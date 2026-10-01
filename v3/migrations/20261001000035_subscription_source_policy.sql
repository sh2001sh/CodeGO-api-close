-- Freeze source provenance and per-model monetary limits on the entitlement.
-- A model absent from model_limits remains unrestricted, as in the source.
ALTER TABLE v3_commerce.plans
    ADD COLUMN IF NOT EXISTS model_limits jsonb NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(model_limits)='object');
ALTER TABLE v3_commerce.subscriptions
    ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS model_limits jsonb NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(model_limits)='object'),
    ADD COLUMN IF NOT EXISTS model_usage jsonb NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(model_usage)='object');

-- Funding preference affects publication, never user authorization. Publish
-- only when retained funding fields change, not for unrelated UI settings.
CREATE TRIGGER user_funding_preference_invalidate
    AFTER UPDATE OF settings ON v3_identity.users
    FOR EACH ROW WHEN (
        (OLD.settings->'billing_preference') IS DISTINCT FROM (NEW.settings->'billing_preference') OR
        (OLD.settings->'funding_source_order') IS DISTINCT FROM (NEW.settings->'funding_source_order') OR
        (OLD.settings->'subscription_order_ids') IS DISTINCT FROM (NEW.settings->'subscription_order_ids'))
    EXECUTE FUNCTION v3_platform.enqueue_invalidation('catalog','id');
