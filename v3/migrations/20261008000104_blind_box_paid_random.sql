-- Add random paid boxes without relaxing existing guaranteed-credit promises.
ALTER TABLE v3_marketplace.blind_box_batches
 DROP CONSTRAINT blind_box_batches_purpose_check,
 DROP CONSTRAINT blind_box_batches_check3,
 ADD CONSTRAINT blind_box_batches_purpose_check CHECK(purpose IN('consumption','credits','paid_random')),
 ADD CONSTRAINT blind_box_batches_pricing_check CHECK(
  (purpose='consumption' AND price_micro=0 AND base_credits_micro=0)
  OR (purpose='credits' AND price_micro>0 AND base_credits_micro>=price_micro)
  OR (purpose='paid_random' AND price_micro>0 AND base_credits_micro=0)
 );

ALTER TABLE v3_marketplace.blind_box_batches ADD COLUMN pity_policy jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE v3_marketplace.blind_box_batches ADD CONSTRAINT blind_box_batches_pity_policy_check CHECK(
 CASE WHEN purpose='paid_random' THEN price_micro<=4611686018427387903 AND
 pity_policy=jsonb_build_object('small_after',10,'small_minimum_micro',price_micro,'big_after',50,'big_minimum_micro',price_micro::numeric*2)
 ELSE pity_policy='{}'::jsonb END
);
ALTER TABLE v3_marketplace.blind_box_open_records
 ADD COLUMN guarantee_credits_micro bigint NOT NULL DEFAULT 0 CHECK(guarantee_credits_micro>=0);
CREATE TABLE v3_marketplace.blind_box_batch_pity (
 user_id bigint PRIMARY KEY REFERENCES v3_identity.users(id),
 opened bigint NOT NULL DEFAULT 0 CHECK(opened>=0),
 small_progress integer NOT NULL DEFAULT 0 CHECK(small_progress>=0 AND small_progress<10),
 big_progress integer NOT NULL DEFAULT 0 CHECK(big_progress>=0 AND big_progress<50)
);
CREATE INDEX blind_box_paid_daily ON v3_marketplace.blind_box_open_records(user_id,created_at) WHERE batch_id IS NOT NULL;
