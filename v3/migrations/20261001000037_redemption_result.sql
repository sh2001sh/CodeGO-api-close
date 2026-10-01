-- Persist the exact response alongside the claimed code and its benefit.
-- Retries after plan edits or box consumption cannot alter the prior grant.
ALTER TABLE v3_commerce.redemption_codes ADD COLUMN redeem_result jsonb;
