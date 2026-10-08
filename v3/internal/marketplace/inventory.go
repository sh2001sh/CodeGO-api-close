package marketplace

import (
	"context"
	"time"
)

type InventoryGroup struct {
	PoolID          int64      `json:"pool_id"`
	PoolName        string     `json:"pool_name"`
	AvailableCount  int64      `json:"available_count"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	DrawCurrentPool bool       `json:"draw_current_pool"`
}

// These predicates are shared by the inventory summary and opening selection.
// A delayed or canceled cash order cannot become spendable inventory merely
// because its item row is still marked available.
const openableInventoryFrom = ` FROM v3_marketplace.blind_box_items i
 JOIN v3_marketplace.blind_box_purchases p ON p.id=i.purchase_id
 JOIN v3_marketplace.blind_box_pools b ON b.id=i.pool_id
 LEFT JOIN v3_marketplace.blind_box_orders o ON o.id=p.external_order_id `

const openableInventoryWhere = ` WHERE i.owner_user_id=$1 AND i.status='available'
 AND p.status='completed' AND (i.expires_at IS NULL OR i.expires_at>$2)
 AND (p.external_order_id IS NULL OR o.status IN('success','completed')
 AND (o.expires_at IS NULL OR o.expires_at>$2)) `

func (s *Service) loadInventoryGroups(ctx context.Context, userID int64) ([]InventoryGroup, error) {
	rows, err := s.pool.Query(ctx, `SELECT i.pool_id,b.name,count(*),min(LEAST(i.expires_at,o.expires_at)),i.draw_current_pool`+
		openableInventoryFrom+openableInventoryWhere+`
 GROUP BY i.pool_id,b.name,i.draw_current_pool
 ORDER BY min(LEAST(i.expires_at,o.expires_at)) NULLS LAST,i.pool_id,i.draw_current_pool`, userID, s.cfg.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]InventoryGroup, 0)
	for rows.Next() {
		var group InventoryGroup
		if err := rows.Scan(&group.PoolID, &group.PoolName, &group.AvailableCount, &group.ExpiresAt, &group.DrawCurrentPool); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}
