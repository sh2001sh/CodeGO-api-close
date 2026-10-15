-- Exhausted provenance remains immutable evidence, but contributes zero to
-- transfer/refund restrictions. Keep restriction reads proportional to live
-- restricted lots rather than every historical funding record.
CREATE INDEX funding_lots_active_restrictions_idx
    ON v3_billing.funding_lots(account_id)
    INCLUDE (remaining_amount, non_transferable, non_refundable)
    WHERE remaining_amount > 0 AND (non_transferable OR non_refundable);
