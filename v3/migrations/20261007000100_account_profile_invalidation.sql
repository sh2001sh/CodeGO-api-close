-- Only account identities used by request funding belong in the profile.
-- Commission posting and unchanged upserts must not recompile the catalog.
DROP TRIGGER billing_account_profile_invalidate ON v3_billing.accounts;

CREATE FUNCTION v3_platform.invalidate_billing_account_profile() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND
       ROW(OLD.owner_type, OLD.owner_id, OLD.kind) IS NOT DISTINCT FROM
       ROW(NEW.owner_type, NEW.owner_id, NEW.kind) THEN
        RETURN NULL;
    END IF;

    IF TG_OP <> 'INSERT' AND
       ((OLD.owner_type = 'user' AND OLD.kind = 'wallet') OR
        (OLD.owner_type = 'api_key' AND OLD.kind = 'key_budget') OR
        (OLD.owner_type = 'subscription' AND OLD.kind = 'subscription')) THEN
        INSERT INTO v3_platform.cache_invalidation_outbox(entity, entity_id)
        VALUES ('account_profile', OLD.owner_id::text);
    END IF;

    IF TG_OP <> 'DELETE' AND
       ((NEW.owner_type = 'user' AND NEW.kind = 'wallet') OR
        (NEW.owner_type = 'api_key' AND NEW.kind = 'key_budget') OR
        (NEW.owner_type = 'subscription' AND NEW.kind = 'subscription')) THEN
        INSERT INTO v3_platform.cache_invalidation_outbox(entity, entity_id)
        VALUES ('account_profile', NEW.owner_id::text);
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER billing_account_profile_invalidate
    AFTER INSERT OR DELETE OR UPDATE OF owner_type, owner_id, kind
    ON v3_billing.accounts
    FOR EACH ROW EXECUTE FUNCTION v3_platform.invalidate_billing_account_profile();
