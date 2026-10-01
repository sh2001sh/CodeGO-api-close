-- Compile account/key authorization when a snapshot or key cache is loaded.
-- Self-editable user settings are deliberately excluded from authorization.
CREATE INDEX usage_logs_key_amount_idx ON v3_billing.usage_logs(key_id,user_id) INCLUDE(amount);
DROP TRIGGER api_keys_invalidate ON v3_identity.api_keys;
ALTER TABLE v3_identity.api_keys ALTER COLUMN max_marketplace_multiplier TYPE numeric(20,6);
CREATE TRIGGER api_keys_invalidate
 AFTER INSERT OR DELETE OR UPDATE OF status, group_name, cross_group_retry, allowed_models,
 allowed_cidrs, budget_limited, max_marketplace_multiplier, expires_at, deleted_at
 ON v3_identity.api_keys FOR EACH ROW EXECUTE FUNCTION v3_platform.enqueue_invalidation('api_key','id');
ALTER TABLE v3_identity.api_keys ADD CONSTRAINT api_key_multiplier_int64
 CHECK(max_marketplace_multiplier<=9223372036854.775807);
CREATE FUNCTION v3_identity.allowed_groups(p_user_id bigint) RETURNS SETOF text
LANGUAGE sql STABLE AS $$
WITH owner AS (
 SELECT group_name FROM v3_identity.users WHERE id=p_user_id AND status='active' AND deleted_at IS NULL
), configured AS (
 SELECT coalesce((SELECT value FROM v3_platform.settings WHERE key='UserUsableGroups' AND NOT sensitive),
                 '{"default":"默认分组","vip":"vip分组"}'::jsonb) AS global_groups,
        coalesce((SELECT value FROM v3_platform.settings WHERE key='group_ratio_setting.group_special_usable_group' AND NOT sensitive),
                 (SELECT value->'group_special_usable_group' FROM v3_platform.settings WHERE key='group_ratio_setting' AND NOT sensitive),
                 '{}'::jsonb) AS special_groups
), special AS (
 SELECT key FROM configured,owner,LATERAL jsonb_each(coalesce(special_groups->owner.group_name,'{}'::jsonb))
), official AS (
 SELECT global_config.key AS name FROM configured,LATERAL jsonb_each(global_groups) global_config
 WHERE NOT EXISTS(SELECT 1 FROM special WHERE special.key='-:'||global_config.key)
 UNION SELECT CASE WHEN key LIKE '+:%' THEN substring(key FROM 3) ELSE key END FROM special WHERE key NOT LIKE '-:%'
), allowed AS (
 SELECT group_name AS name FROM owner
 UNION SELECT o.name FROM official o JOIN v3_catalog.groups g ON g.name=o.name
 WHERE NOT EXISTS(SELECT 1 FROM v3_channelmarket.groups m WHERE m.internal_group_name=o.name)
 AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.route_pools p WHERE p.internal_group_name=o.name)
 UNION SELECT m.internal_group_name FROM v3_channelmarket.groups m JOIN v3_catalog.channels c ON c.id=m.channel_id
 WHERE m.deleted_at IS NULL AND m.lifecycle_status='active' AND c.status='enabled'
 AND (m.visibility='public' OR m.owner_user_id=p_user_id OR EXISTS(SELECT 1 FROM v3_channelmarket.group_access a WHERE a.group_id=m.id AND a.user_id=p_user_id))
 AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks b WHERE b.channel_id=m.channel_id AND b.user_id=p_user_id)
 UNION SELECT internal_group_name FROM v3_channelmarket.route_pools WHERE owner_user_id=p_user_id
)
SELECT name FROM allowed WHERE name IS NOT NULL AND name<>'' AND EXISTS(SELECT 1 FROM owner) ORDER BY name;
$$;

-- Policy edits affect cached key profiles as well as the catalog snapshot.
CREATE TRIGGER identity_market_access_invalidate AFTER INSERT OR UPDATE OR DELETE
 ON v3_channelmarket.group_access FOR EACH ROW
 EXECUTE FUNCTION v3_platform.enqueue_invalidation('user','user_id');
CREATE TRIGGER identity_market_blocks_invalidate AFTER INSERT OR UPDATE OR DELETE
 ON v3_channelmarket.channel_user_blocks FOR EACH ROW
 EXECUTE FUNCTION v3_platform.enqueue_invalidation('user','user_id');
CREATE TRIGGER identity_personal_pools_invalidate AFTER INSERT OR UPDATE OR DELETE
 ON v3_channelmarket.route_pools FOR EACH ROW
 EXECUTE FUNCTION v3_platform.enqueue_invalidation('user','owner_user_id');

CREATE FUNCTION v3_identity.invalidate_group_policies() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE row_data jsonb;
BEGIN
 IF TG_TABLE_SCHEMA='v3_platform' THEN
  row_data:=CASE WHEN TG_OP='DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END;
  IF row_data->>'key' NOT IN ('UserUsableGroups','AutoGroups',
   'group_ratio_setting.group_special_usable_group','group_ratio_setting') THEN RETURN NULL; END IF;
 END IF;
 INSERT INTO v3_platform.cache_invalidation_outbox(entity,entity_id)
 SELECT 'user',id::text FROM v3_identity.users WHERE deleted_at IS NULL;
 RETURN NULL;
END;
$$;
CREATE TRIGGER identity_official_groups_invalidate AFTER INSERT OR UPDATE OR DELETE
 ON v3_catalog.groups FOR EACH STATEMENT EXECUTE FUNCTION v3_identity.invalidate_group_policies();
CREATE TRIGGER identity_market_groups_invalidate AFTER INSERT OR DELETE OR UPDATE OF visibility,lifecycle_status,owner_user_id,deleted_at
 ON v3_channelmarket.groups FOR EACH STATEMENT EXECUTE FUNCTION v3_identity.invalidate_group_policies();
CREATE TRIGGER identity_channel_status_invalidate AFTER UPDATE OF status
 ON v3_catalog.channels FOR EACH STATEMENT EXECUTE FUNCTION v3_identity.invalidate_group_policies();
CREATE TRIGGER identity_group_settings_invalidate AFTER INSERT OR UPDATE OR DELETE
 ON v3_platform.settings FOR EACH ROW EXECUTE FUNCTION v3_identity.invalidate_group_policies();

CREATE FUNCTION v3_identity.auto_groups(p_user_id bigint) RETURNS SETOF text
LANGUAGE sql STABLE AS $$
SELECT allowed FROM v3_identity.allowed_groups(p_user_id) allowed
WHERE allowed IN (SELECT jsonb_array_elements_text(coalesce(
 (SELECT value FROM v3_platform.settings WHERE key='AutoGroups' AND NOT sensitive),'["default"]'::jsonb)))
ORDER BY allowed;
$$;
