-- Existing withdrawable affiliate earnings are current monetary liabilities.
-- Retain their own account so withdrawal debits the source instead of minting
-- a wallet credit. No new referral accrual is introduced.
ALTER TABLE v3_billing.accounts DROP CONSTRAINT accounts_kind_check;
ALTER TABLE v3_billing.accounts ADD CONSTRAINT accounts_kind_check CHECK (kind IN (
    'wallet', 'key_budget', 'subscription', 'marketplace_pending',
    'marketplace_earned', 'platform_revenue', 'affiliate'
));
