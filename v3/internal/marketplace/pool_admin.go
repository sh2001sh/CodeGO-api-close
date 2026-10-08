package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
)

// AdminPools includes disabled pools, so disabling a pool does not remove the
// administrator's ability to inspect its policy or enable it again.
func (s *Service) AdminPools(ctx context.Context) ([]Pool, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,name,enabled,price_micro,daily_limit,rewards,guarantees,scope,monthly_limit,daily_open_limit,standard_policy FROM v3_marketplace.blind_box_pools ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pools := make([]Pool, 0)
	for rows.Next() {
		var p Pool
		var rewards, guarantees, standard []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.Enabled, &p.Price, &p.DailyLimit, &rewards, &guarantees, &p.Scope, &p.MonthlyLimit, &p.DailyOpenLimit, &standard); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rewards, &p.Rewards); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(guarantees, &p.Guarantees); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(standard, &p.Standard); err != nil {
			return nil, err
		}
		if err := validatePool(p); err != nil {
			return nil, fmt.Errorf("marketplace: stored pool %d violates invariants: %v", p.ID, err)
		}
		pools = append(pools, p)
	}
	return pools, rows.Err()
}
