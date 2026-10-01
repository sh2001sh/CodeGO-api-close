package commerce

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

type SubscriptionPreference struct {
	BillingPreference    string   `json:"billing_preference"`
	FundingSourceOrder   []string `json:"funding_source_order"`
	SubscriptionOrderIDs []int64  `json:"subscription_order_ids"`
}

func normalizePreference(p SubscriptionPreference) (SubscriptionPreference, error) {
	if len(p.FundingSourceOrder) == 0 {
		switch p.BillingPreference {
		case "", "subscription_first":
			p.FundingSourceOrder = []string{"subscription", "wallet"}
		case "wallet_first":
			p.FundingSourceOrder = []string{"wallet", "subscription"}
		case "subscription_only":
			p.FundingSourceOrder = []string{"subscription"}
		case "wallet_only":
			p.FundingSourceOrder = []string{"wallet"}
		default:
			return p, ErrInvalid
		}
	}
	if len(p.FundingSourceOrder) > 2 || len(p.SubscriptionOrderIDs) > 1000 {
		return p, ErrInvalid
	}
	seen := map[string]bool{}
	for _, source := range p.FundingSourceOrder {
		if (source != "wallet" && source != "subscription") || seen[source] {
			return p, ErrInvalid
		}
		seen[source] = true
	}
	p.BillingPreference = p.FundingSourceOrder[0] + "_only"
	if len(p.FundingSourceOrder) == 2 {
		p.BillingPreference = p.FundingSourceOrder[0] + "_first"
	}
	ids := map[int64]bool{}
	for _, id := range p.SubscriptionOrderIDs {
		if id <= 0 || ids[id] {
			return p, ErrInvalid
		}
		ids[id] = true
	}
	if p.SubscriptionOrderIDs == nil {
		p.SubscriptionOrderIDs = []int64{}
	}
	return p, nil
}

func (s *Service) SubscriptionPreference(ctx context.Context, userID int64) (SubscriptionPreference, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT settings FROM v3_identity.users WHERE id=$1`, userID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return SubscriptionPreference{}, ErrNotFound
	}
	if err != nil {
		return SubscriptionPreference{}, err
	}
	var p SubscriptionPreference
	if err = json.Unmarshal(raw, &p); err != nil {
		return p, err
	}
	return normalizePreference(p)
}

func (s *Service) SetSubscriptionPreference(ctx context.Context, userID int64, p SubscriptionPreference) (SubscriptionPreference, error) {
	if userID <= 0 {
		return p, ErrInvalid
	}
	p, err := normalizePreference(p)
	if err != nil {
		return p, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id int64
		if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, userID).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		for _, sub := range p.SubscriptionOrderIDs {
			var owned bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.subscriptions
			 WHERE id=$1 AND user_id=$2 AND state='active' AND expires_at>$3)`, sub, userID, s.cfg.Now()).Scan(&owned); err != nil {
				return err
			}
			if !owned {
				return ErrNotFound
			}
		}
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE v3_identity.users SET settings=settings||$2::jsonb WHERE id=$1`, userID, string(raw))
		return err
	})
	return p, err
}

// FundingPreferences is refreshed beside ActiveFundingSources off the request
// path. Keys are users, values the legacy preference plus ordered account ids.
// Expired ids in stored preferences are omitted, never revived.
func (s *Service) FundingPreferences(ctx context.Context) (map[int64]string, map[int64][]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,settings FROM v3_identity.users
	 WHERE settings ? 'billing_preference' OR settings ? 'funding_source_order' OR settings ? 'subscription_order_ids'`)
	if err != nil {
		return nil, nil, err
	}
	prefs := map[int64]string{}
	orders := map[int64][]int64{}
	for rows.Next() {
		var id int64
		var raw []byte
		var p SubscriptionPreference
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if err = json.Unmarshal(raw, &p); err != nil {
			rows.Close()
			return nil, nil, err
		}
		p, err = normalizePreference(p)
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		prefs[id] = p.BillingPreference
		orders[id] = p.SubscriptionOrderIDs
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	accounts, err := s.pool.Query(ctx, `SELECT id,user_id,account_id FROM v3_commerce.subscriptions WHERE state='active' AND starts_at<=$1 AND expires_at>$1`, s.cfg.Now())
	if err != nil {
		return nil, nil, err
	}
	defer accounts.Close()
	byUser := map[int64]map[int64]int64{}
	for accounts.Next() {
		var id, user, account int64
		if err = accounts.Scan(&id, &user, &account); err != nil {
			return nil, nil, err
		}
		if byUser[user] == nil {
			byUser[user] = map[int64]int64{}
		}
		byUser[user][id] = account
	}
	for user, ids := range orders {
		var ordered []int64
		for _, id := range ids {
			if account := byUser[user][id]; account > 0 {
				ordered = append(ordered, account)
			}
		}
		orders[user] = ordered
	}
	return prefs, orders, accounts.Err()
}
