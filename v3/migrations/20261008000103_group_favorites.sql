-- Account-owned market group bookmarks. Model favorites remain historical data.
CREATE TABLE v3_channelmarket.group_favorites (
    user_id bigint NOT NULL REFERENCES v3_identity.users(id) ON DELETE CASCADE,
    group_id text NOT NULL REFERENCES v3_channelmarket.groups(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, group_id)
);
CREATE INDEX group_favorites_recent ON v3_channelmarket.group_favorites
    (user_id, created_at DESC, group_id);
