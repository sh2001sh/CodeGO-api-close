package catalog

import (
	"context"
	"math"

	"github.com/jackc/pgx/v5"
)

func readMarketRankings(ctx context.Context, tx pgx.Tx, groups map[string]MarketGroupPolicy, names map[string]string) error {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON(group_id) group_id,score,request_count,observing FROM v3_channelmarket.ranking_snapshots
	 WHERE window_hours=24 AND ranking_version IN ('v3-usage','marketplace-v7-consumer-cost') ORDER BY group_id,calculated_at DESC,id DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var score float64
		var requests int64
		var observing bool
		if err = rows.Scan(&id, &score, &requests, &observing); err != nil {
			return err
		}
		if name, exists := names[id]; exists {
			group := groups[name]
			group.Score, group.HasScore = score, requests > 0 && !observing && !math.IsNaN(score) && !math.IsInf(score, 0)
			groups[name] = group
		}
	}
	return rows.Err()
}

func readMarketPools(ctx context.Context, tx pgx.Tx, pools map[string]MarketPoolPolicy) error {
	rows, err := tx.Query(ctx, `SELECT internal_group_name,owner_user_id,max_multiplier_ppm,max_attempts,strategy FROM v3_channelmarket.route_pools ORDER BY id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		var pool MarketPoolPolicy
		if err = rows.Scan(&name, &pool.OwnerUserID, &pool.MaxMultiplierPPM, &pool.MaxAttempts, &pool.Strategy); err != nil {
			rows.Close()
			return err
		}
		pools[name] = pool
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `SELECT p.internal_group_name,m.group_id,coalesce(m.catalog_group_name,g.internal_group_name,''),m.priority
	 FROM v3_channelmarket.route_pool_members m JOIN v3_channelmarket.route_pools p ON p.id=m.pool_id
	 LEFT JOIN v3_channelmarket.groups g ON g.id=m.group_id ORDER BY p.id,m.priority,m.group_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var member MarketPoolMember
		if err = rows.Scan(&name, &member.GroupID, &member.CatalogGroupName, &member.Priority); err != nil {
			return err
		}
		pool := pools[name]
		pool.GroupIDs = append(pool.GroupIDs, member.GroupID)
		pool.Members = append(pool.Members, member)
		pools[name] = pool
	}
	return rows.Err()
}
