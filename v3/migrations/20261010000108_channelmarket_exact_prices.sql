-- V2 allowed zero public rates and precise positive negotiated rates. Preserve
-- their charging semantics; scaled PPM numeric values are never rounded.
ALTER TABLE v3_channelmarket.groups
    DROP CONSTRAINT groups_multiplier_ppm_check,
    ADD CONSTRAINT groups_multiplier_ppm_check CHECK(multiplier_ppm >= 0);

ALTER TABLE v3_channelmarket.user_multipliers
    ALTER COLUMN multiplier_ppm TYPE numeric USING multiplier_ppm::numeric,
    ADD CONSTRAINT user_multipliers_exact_range_check
        CHECK(multiplier_ppm <= 9223372036854775807);

ALTER TABLE v3_channelmarket.time_range_multipliers
    ALTER COLUMN multiplier_ppm TYPE numeric USING multiplier_ppm::numeric,
    ADD CONSTRAINT time_range_multipliers_exact_range_check
        CHECK(multiplier_ppm <= 9223372036854775807);

ALTER TABLE v3_channelmarket.multiplier_notices
    ALTER COLUMN previous_ppm TYPE numeric USING previous_ppm::numeric,
    ALTER COLUMN multiplier_ppm TYPE numeric USING multiplier_ppm::numeric,
    ADD CONSTRAINT multiplier_notices_previous_range_check
        CHECK(previous_ppm >= 0 AND previous_ppm <= 9223372036854775807),
    ADD CONSTRAINT multiplier_notices_exact_range_check
        CHECK(multiplier_ppm >= 0 AND multiplier_ppm <= 9223372036854775807);

ALTER TABLE v3_channelmarket.bargain_requests
    ALTER COLUMN proposed_ppm TYPE numeric USING proposed_ppm::numeric,
    ADD CONSTRAINT bargain_requests_exact_range_check
        CHECK(proposed_ppm <= 9223372036854775807);

-- A never-started online run already has LIKE staging tables from revision 107.
-- Keep those definitions identical so copy and final adoption preserve prices.
DO $$
BEGIN
    IF to_regclass('v3_migration_online.v3_channelmarket__groups') IS NOT NULL THEN
        ALTER TABLE v3_migration_online.v3_channelmarket__groups
            DROP CONSTRAINT groups_multiplier_ppm_check,
            ADD CONSTRAINT groups_multiplier_ppm_check CHECK(multiplier_ppm >= 0);
    END IF;
    IF to_regclass('v3_migration_online.v3_channelmarket__user_multipliers') IS NOT NULL THEN
        ALTER TABLE v3_migration_online.v3_channelmarket__user_multipliers
            ALTER COLUMN multiplier_ppm TYPE numeric USING multiplier_ppm::numeric,
            ADD CONSTRAINT user_multipliers_exact_range_check
                CHECK(multiplier_ppm <= 9223372036854775807);
    END IF;
    IF to_regclass('v3_migration_online.v3_channelmarket__multiplier_notices') IS NOT NULL THEN
        ALTER TABLE v3_migration_online.v3_channelmarket__multiplier_notices
            ALTER COLUMN previous_ppm TYPE numeric USING previous_ppm::numeric,
            ALTER COLUMN multiplier_ppm TYPE numeric USING multiplier_ppm::numeric,
            ADD CONSTRAINT multiplier_notices_previous_range_check
                CHECK(previous_ppm >= 0 AND previous_ppm <= 9223372036854775807),
            ADD CONSTRAINT multiplier_notices_exact_range_check
                CHECK(multiplier_ppm >= 0 AND multiplier_ppm <= 9223372036854775807);
    END IF;
    IF to_regclass('v3_migration_online.v3_channelmarket__time_range_multipliers') IS NOT NULL THEN
        ALTER TABLE v3_migration_online.v3_channelmarket__time_range_multipliers
            ALTER COLUMN multiplier_ppm TYPE numeric USING multiplier_ppm::numeric,
            ADD CONSTRAINT time_range_multipliers_exact_range_check
                CHECK(multiplier_ppm <= 9223372036854775807);
    END IF;
    IF to_regclass('v3_migration_online.v3_channelmarket__bargain_requests') IS NOT NULL THEN
        ALTER TABLE v3_migration_online.v3_channelmarket__bargain_requests
            ALTER COLUMN proposed_ppm TYPE numeric USING proposed_ppm::numeric,
            ADD CONSTRAINT bargain_requests_exact_range_check
                CHECK(proposed_ppm <= 9223372036854775807);
    END IF;
END $$;
