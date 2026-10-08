package marketplace

import (
	"context"
	"encoding/json"
	"errors"

	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type Purchase struct {
	ID        int64         `json:"id"`
	Quantity  int           `json:"quantity"`
	UnitPrice credits.Micro `json:"unit_price_micro"`
	Total     credits.Micro `json:"total_micro"`
}
type OpenRecord struct {
	BatchID   int64     `json:"batch_id,omitempty"`
	ID        int64     `json:"id"`
	ItemID    int64     `json:"item_id"`
	Reward    Reward    `json:"reward"`
	PropID    int64     `json:"prop_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Guarantee string    `json:"guarantee_type"`
}

func (s *Service) PurchaseBoxes(ctx context.Context, userID int64, requestID string, poolID int64, count int) (Purchase, error) {
	var purchase Purchase
	if err := validRequest(userID, requestID, count); err != nil {
		return purchase, err
	}
	if poolID <= 0 {
		return purchase, ErrInvalidInput
	}
	account, err := s.wallets.WalletAccount(ctx, userID)
	if err != nil {
		return purchase, err
	}
	input := struct {
		PoolID int64
		Count  int
	}{poolID, count}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockUser(ctx, tx, userID); err != nil {
			return err
		}
		found, err := replay(ctx, tx, userID, "purchase", requestID, input, &purchase)
		if err != nil || found {
			return err
		}
		p, err := loadPool(ctx, tx, poolID)
		if err != nil {
			return err
		}
		if !p.Enabled {
			return ErrUnavailable
		}
		if int64(p.Price) > math.MaxInt64/int64(count) {
			return credits.ErrOverflow
		}
		if err := s.checkPurchaseLimitsTx(ctx, tx, userID, p, count); err != nil {
			return err
		}
		purchase, err = s.chargeAndCreatePurchaseTx(ctx, tx, account, userID, poolID, requestID, count, p)
		if err != nil {
			return err
		}
		return remember(ctx, tx, userID, "purchase", requestID, input, purchase)
	})
	return purchase, err
}

// chargeAndCreatePurchaseTx charges the buyer, records the purchase row, and
// creates one unopened inventory item per unit purchased.
func (s *Service) chargeAndCreatePurchaseTx(ctx context.Context, tx pgx.Tx, account, userID, poolID int64, requestID string, count int, p Pool) (Purchase, error) {
	purchase := Purchase{Quantity: count, UnitPrice: p.Price, Total: p.Price * credits.Micro(count)}
	if _, err := s.money.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: -purchase.Total, Kind: "transfer", OperationID: operation("purchase", userID, requestID, 0), Reason: "blind_box_purchase"}); err != nil {
		return purchase, err
	}
	day := s.cfg.Now().In(s.cfg.Location).Format("2006-01-02")
	if err := tx.QueryRow(ctx, `INSERT INTO v3_marketplace.blind_box_purchases(user_id,pool_id,quantity,unit_price_micro,purchase_date,request_id,total_micro) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, userID, poolID, count, p.Price, day, "purchase:"+requestID, purchase.Total).Scan(&purchase.ID); err != nil {
		return purchase, err
	}
	rewards, err := json.Marshal(p.Rewards)
	if err != nil {
		return purchase, err
	}
	guarantees, err := json.Marshal(p.Guarantees)
	if err != nil {
		return purchase, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_items(purchase_id,owner_user_id,purchase_user_id,pool_id,rewards,guarantees) SELECT $1,$2,$2,$3,$4,$6 FROM generate_series(1,$5::int)`, purchase.ID, userID, poolID, rewards, count, guarantees)
	return purchase, err
}

func loadPool(ctx context.Context, tx pgx.Tx, id int64) (Pool, error) {
	var p Pool
	var rewards, guarantees, standard []byte
	err := tx.QueryRow(ctx, `SELECT id,name,enabled,price_micro,daily_limit,rewards,guarantees,scope,monthly_limit,daily_open_limit,standard_policy FROM v3_marketplace.blind_box_pools WHERE id=$1`, id).Scan(&p.ID, &p.Name, &p.Enabled, &p.Price, &p.DailyLimit, &rewards, &guarantees, &p.Scope, &p.MonthlyLimit, &p.DailyOpenLimit, &standard)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(rewards, &p.Rewards); err != nil {
		return p, err
	}
	if err := json.Unmarshal(guarantees, &p.Guarantees); err != nil {
		return p, err
	}
	if err := json.Unmarshal(standard, &p.Standard); err != nil {
		return p, err
	}
	return p, validatePool(p)
}

func (s *Service) SavePool(ctx context.Context, p Pool) (Pool, error) {
	if p.Scope == "" {
		p.Scope = "credits"
	}
	if err := validatePool(p); err != nil {
		return p, err
	}
	payload, err := json.Marshal(p.Rewards)
	if err != nil {
		return p, err
	}
	guarantees, err := json.Marshal(p.Guarantees)
	if err != nil {
		return p, err
	}
	standard, err := json.Marshal(p.Standard)
	if err != nil {
		return p, err
	}
	if p.ID == 0 {
		err = s.pool.QueryRow(ctx, `INSERT INTO v3_marketplace.blind_box_pools(name,enabled,price_micro,daily_limit,rewards,guarantees,scope,monthly_limit,daily_open_limit,standard_policy) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`, p.Name, p.Enabled, p.Price, p.DailyLimit, payload, guarantees, p.Scope, p.MonthlyLimit, p.DailyOpenLimit, standard).Scan(&p.ID)
	} else {
		tag, e := s.pool.Exec(ctx, `UPDATE v3_marketplace.blind_box_pools SET name=$2,enabled=$3,price_micro=$4,daily_limit=$5,rewards=$6,updated_at=$7,guarantees=$8,scope=$9,monthly_limit=$10,daily_open_limit=$11,standard_policy=$12 WHERE id=$1`, p.ID, p.Name, p.Enabled, p.Price, p.DailyLimit, payload, s.cfg.Now(), guarantees, p.Scope, p.MonthlyLimit, p.DailyOpenLimit, standard)
		err = e
		if err == nil && tag.RowsAffected() != 1 {
			err = ErrNotFound
		}
	}
	return p, err
}
