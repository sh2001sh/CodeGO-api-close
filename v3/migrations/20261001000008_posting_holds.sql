-- The business transaction reserves its fixed debit before committing PG.
-- Outbox delivery applies the delta and releases this hold in one Redis script.
ALTER TABLE v3_billing.balance_outbox ADD COLUMN reservation_id text;
CREATE INDEX balance_outbox_account ON v3_billing.balance_outbox(account_id);
