package marketplace

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Service) purchaseWindows() (time.Time, time.Time, time.Time, time.Time) {
	now := s.cfg.Now().In(s.cfg.Location)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.cfg.Location)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, s.cfg.Location)
	return day, day.AddDate(0, 0, 1), month, month.AddDate(0, 1, 0)
}

// checkPurchaseLimitsTx runs after the shared user lock in both payment and
// wallet checkout. External orders are counted at creation (including pending)
// and their materialized purchases are excluded to avoid counting twice.
func (s *Service) checkPurchaseLimitsTx(ctx context.Context, tx pgx.Tx, userID int64, p Pool, count int) error {
	if userID <= 0 || count < 1 || p.DailyLimit < 0 || p.MonthlyLimit < 0 {
		return ErrInvalidInput
	}
	if p.DailyLimit == 0 && p.MonthlyLimit == 0 {
		return nil
	}
	day, nextDay, month, nextMonth := s.purchaseWindows()
	var daily, monthly int64
	err := tx.QueryRow(ctx, `WITH cash AS (
 SELECT coalesce(sum(quantity) FILTER(WHERE created_at>=$2 AND created_at<$3),0) daily,
        coalesce(sum(quantity),0) monthly
 FROM v3_marketplace.blind_box_orders WHERE user_id=$1 AND source='purchase'
 AND status IN('pending','success','completed') AND created_at>=$4 AND created_at<$5
 ), wallet AS (
 SELECT coalesce(sum(quantity) FILTER(WHERE purchase_date=$6::date),0) daily,
        coalesce(sum(quantity),0) monthly
 FROM v3_marketplace.blind_box_purchases WHERE user_id=$1 AND NOT is_grant
 AND external_order_id IS NULL AND status='completed' AND purchase_date>=$7::date AND purchase_date<$8::date
 ) SELECT cash.daily+wallet.daily,cash.monthly+wallet.monthly FROM cash,wallet`, userID,
		day, nextDay, month, nextMonth, day.Format("2006-01-02"), month.Format("2006-01-02"), nextMonth.Format("2006-01-02")).Scan(&daily, &monthly)
	if err != nil {
		return err
	}
	if p.DailyLimit > 0 && (daily > int64(p.DailyLimit) || int64(count) > int64(p.DailyLimit)-daily) {
		return ErrDailyLimit
	}
	if p.MonthlyLimit > 0 && (monthly > int64(p.MonthlyLimit) || int64(count) > int64(p.MonthlyLimit)-monthly) {
		return ErrMonthlyLimit
	}
	return nil
}

// Legacy standard opens share a site-wide limit. Its day lock stays held until
// the caller commits every draw, preventing different users claiming one slot.
// Other pools keep their per-user limit. Grants and reward draws count too.
func (s *Service) checkOpenLimitsTx(ctx context.Context, tx pgx.Tx, userID int64, pools map[int64]int) error {
	if userID <= 0 {
		return ErrInvalidInput
	}
	ids := make([]int64, 0, len(pools))
	for poolID, count := range pools {
		if poolID <= 0 || count < 1 {
			return ErrInvalidInput
		}
		ids = append(ids, poolID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	day, nextDay, _, _ := s.purchaseWindows()
	loaded := make(map[int64]Pool, len(ids))
	var standardSelected int64
	globalLimit := false
	for _, poolID := range ids {
		p, err := loadPool(ctx, tx, poolID)
		if err != nil {
			return err
		}
		loaded[poolID] = p
		if p.Scope == "standard" {
			standardSelected += int64(pools[poolID])
			globalLimit = globalLimit || p.Standard.Enabled && p.DailyOpenLimit > 0
		}
	}
	var standardOpened int64
	if globalLimit {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "marketplace:standard-open:"+day.Format("2006-01-02")); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM v3_marketplace.blind_box_open_records r
 JOIN v3_marketplace.blind_box_pools p ON p.id=r.pool_id WHERE p.scope='standard' AND r.created_at>=$1 AND r.created_at<$2`, day, nextDay).Scan(&standardOpened); err != nil {
			return err
		}
	}
	for _, poolID := range ids {
		p := loaded[poolID]
		if p.DailyOpenLimit == 0 {
			continue
		}
		if p.Scope == "standard" && p.Standard.Enabled {
			if standardOpened > int64(p.DailyOpenLimit) || standardSelected > int64(p.DailyOpenLimit)-standardOpened {
				return ErrOpenLimit
			}
			continue
		}
		var opened int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM v3_marketplace.blind_box_open_records
 WHERE user_id=$1 AND pool_id=$2 AND created_at>=$3 AND created_at<$4`, userID, poolID, day, nextDay).Scan(&opened); err != nil {
			return err
		}
		if opened > int64(p.DailyOpenLimit) || int64(pools[poolID]) > int64(p.DailyOpenLimit)-opened {
			return ErrOpenLimit
		}
	}
	return nil
}
