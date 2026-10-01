-- A newly created wallet must replace a previously compiled profile with no
-- wallet. Balance churn is intentionally excluded from catalog invalidation.
CREATE TRIGGER billing_account_profile_invalidate
    AFTER INSERT OR DELETE OR UPDATE OF owner_type, owner_id, kind
    ON v3_billing.accounts
    FOR EACH ROW EXECUTE FUNCTION v3_platform.enqueue_invalidation('account_profile', 'owner_id');
