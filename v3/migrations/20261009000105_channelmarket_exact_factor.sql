-- Historical settlement factors are metadata. V2 accepted decimal factors
-- finer than an integral PPM; retain them exactly without changing any money.
-- New native requests continue to write integral PPM values.
ALTER TABLE v3_channelmarket.settlements
    ALTER COLUMN multiplier_ppm TYPE numeric USING multiplier_ppm::numeric,
    ADD CONSTRAINT settlements_multiplier_range_check
        CHECK(multiplier_ppm >= 0 AND multiplier_ppm <= 9223372036854775807);
