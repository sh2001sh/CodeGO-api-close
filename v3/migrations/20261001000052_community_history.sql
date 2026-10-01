-- Keep source identifiers alongside the current one-vote-per-user constraint.
ALTER TABLE v3_community.channel_ratings ADD COLUMN legacy_id bigint UNIQUE;
