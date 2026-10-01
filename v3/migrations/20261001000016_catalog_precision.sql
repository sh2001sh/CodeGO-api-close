-- Multipliers are fixed at six decimal places at the money boundary. The
-- original four-place catalog column must not round imported pricing early.
ALTER TABLE v3_catalog.groups ALTER COLUMN multiplier TYPE numeric(12,6);
