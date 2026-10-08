package desktop

// Desktop discovery and config recommendations share the same visible model
// projection, including retained private route pools and their allowed members.
const availableModels = `WITH direct AS (
 SELECT DISTINCT cg.group_name,cm.model FROM v3_catalog.channels c
 JOIN v3_catalog.channel_groups cg ON cg.channel_id=c.id JOIN v3_catalog.channel_models cm ON cm.channel_id=c.id
 WHERE c.status='enabled' AND cg.group_name IN(SELECT v3_identity.allowed_groups($1))
), available AS (
 SELECT group_name,model FROM direct
 UNION SELECT p.internal_group_name,d.model FROM v3_channelmarket.route_pools p
 JOIN v3_channelmarket.route_pool_members m ON m.pool_id=p.id
 LEFT JOIN v3_channelmarket.groups g ON g.id=m.group_id
 JOIN direct d ON d.group_name=coalesce(m.catalog_group_name,g.internal_group_name)
 WHERE p.owner_user_id=$1 AND p.internal_group_name IN(SELECT v3_identity.allowed_groups($1))
) `
