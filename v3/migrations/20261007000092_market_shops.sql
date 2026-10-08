-- Shops have a public identity independent of private account IDs. Public text
-- remains approved until an administrator reviews a replacement.
CREATE TABLE v3_channelmarket.shops (
    id bigint GENERATED ALWAYS AS IDENTITY (START WITH 10001) PRIMARY KEY,
    owner_user_id bigint NOT NULL UNIQUE REFERENCES v3_identity.users(id),
    name text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    submitted_name text NOT NULL DEFAULT '',
    submitted_description text NOT NULL DEFAULT '',
    review_status text NOT NULL DEFAULT 'approved' CHECK (review_status IN ('approved','pending','rejected')),
    review_reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (char_length(name) <= 40 AND char_length(submitted_name) <= 40),
    CHECK (char_length(description) <= 200 AND char_length(submitted_description) <= 200)
);
INSERT INTO v3_channelmarket.shops(owner_user_id)
SELECT DISTINCT owner_user_id FROM v3_catalog.channels
WHERE scope='marketplace' AND owner_user_id IS NOT NULL ORDER BY owner_user_id;

-- Legacy import and normal channel creation both insert market groups. Create
-- shop identities there so a post-migration import cannot leave owners without
-- a public shop until their first visit to the owner console.
CREATE FUNCTION v3_channelmarket.ensure_owner_shop() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO v3_channelmarket.shops(owner_user_id) VALUES (NEW.owner_user_id)
    ON CONFLICT(owner_user_id) DO NOTHING;
    RETURN NEW;
END;
$$;
CREATE TRIGGER market_group_owner_shop AFTER INSERT OR UPDATE OF owner_user_id
ON v3_channelmarket.groups FOR EACH ROW EXECUTE FUNCTION v3_channelmarket.ensure_owner_shop();
