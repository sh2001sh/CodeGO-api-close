-- Cash box payments grant sealed inventory; no credit grant is attached.
ALTER TABLE v3_commerce.orders ADD COLUMN checkout_qrcode_url text NOT NULL DEFAULT '';
ALTER TABLE v3_commerce.orders DROP CONSTRAINT orders_kind_check;
ALTER TABLE v3_commerce.orders ADD CONSTRAINT orders_kind_check
    CHECK (kind IN ('topup','subscription','blind_box'));
ALTER TABLE v3_commerce.orders DROP CONSTRAINT orders_credits_check;
ALTER TABLE v3_commerce.orders ADD CONSTRAINT orders_credits_check
    CHECK ((kind='blind_box' AND credits=0) OR
           (kind<>'blind_box' AND credits>=0 AND
            (credits>0 OR (kind='subscription' AND period_credits>0))));
