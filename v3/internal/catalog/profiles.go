package catalog

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AccountProfile is computed at catalog publication, never during a request.
// Subscription windows and card expiry are checked locally on every use.
type AccountProfile struct {
	UserID               int64                `json:"user_id"`
	WalletAccountID      int64                `json:"wallet_account_id"`
	MultiplierPPM        int64                `json:"multiplier_ppm"`
	MultiplierExpiresAt  time.Time            `json:"multiplier_expires_at"`
	Cards                []MultiplierCard     `json:"cards,omitempty"`
	Subscriptions        []SubscriptionBucket `json:"subscriptions"`
	AllowedGroups        []string             `json:"allowed_groups"`
	AutoGroups           []string             `json:"auto_groups"`
	KeyBudgetAccounts    map[int64]int64      `json:"key_budget_accounts"`
	BillingPreference    string               `json:"billing_preference"`
	FundingSourceOrder   []string             `json:"funding_source_order"`
	SubscriptionOrderIDs []int64              `json:"subscription_order_ids"`
}

type MultiplierCard struct {
	MultiplierPPM     int64     `json:"multiplier_ppm"`
	ExpiresAt         time.Time `json:"expires_at"`
	ID                int64     `json:"id"`
	PropType          string    `json:"prop_type"`
	MaxDiscountMicro  int64     `json:"max_discount_micro"`
	UsedDiscountMicro int64     `json:"used_discount_micro"`
}

type SubscriptionBucket struct {
	AccountID      int64            `json:"account_id"`
	SubscriptionID int64            `json:"subscription_id"`
	Paid           bool             `json:"paid"`
	Models         []string         `json:"models,omitempty"`
	ModelLimits    map[string]int64 `json:"model_limits,omitempty"`
	ModelUsage     map[string]int64 `json:"model_usage,omitempty"`
	StartsAt       time.Time        `json:"starts_at"`
	ExpiresAt      time.Time        `json:"expires_at"`
}

// CardMultiplier returns the precomputed active discount. Expiry must be
// checked here because snapshots do not necessarily change at expiry time.
func (p AccountProfile) CardMultiplier(now time.Time) float64 {
	if len(p.Cards) > 0 {
		factor := int64(1000000)
		for _, card := range p.Cards {
			legacy := card.ID == 0 && card.PropType == ""
			if (card.IsConsumptionDiscount() || legacy) && card.MultiplierPPM >= 0 && card.MultiplierPPM <= 1000000 && !card.ExpiresAt.IsZero() && now.Before(card.ExpiresAt) {
				factor = min(factor, card.MultiplierPPM)
			}
		}
		return float64(factor) / 1000000
	}
	if p.MultiplierPPM < 0 || p.MultiplierPPM > 1000000 || p.MultiplierExpiresAt.IsZero() || !now.Before(p.MultiplierExpiresAt) {
		return 1
	}
	return float64(p.MultiplierPPM) / 1000000
}

func (p AccountProfile) SubscriptionAccounts(now time.Time) []int64 {
	accounts := make([]int64, 0, len(p.Subscriptions))
	for _, bucket := range p.Subscriptions {
		if !now.Before(bucket.StartsAt) && now.Before(bucket.ExpiresAt) {
			accounts = append(accounts, bucket.AccountID)
		}
	}
	return accounts
}

// loadBaseAccountProfiles loads the per-user base profile fields (wallet
// account, multiplier, cards, groups, funding preference).
func loadBaseAccountProfiles(ctx context.Context, tx pgx.Tx) (map[int64]AccountProfile, error) {
	rows, err := tx.Query(ctx, `SELECT u.id,coalesce(a.id,0),coalesce(p.multiplier_ppm,1000000),p.expires_at,coalesce(p.cards,'[]'::jsonb),ARRAY(SELECT v3_identity.allowed_groups(u.id)),ARRAY(SELECT v3_identity.auto_groups(u.id)),u.settings
	 FROM v3_identity.users u
	 LEFT JOIN v3_billing.accounts a ON a.owner_type='user' AND a.owner_id=u.id AND a.kind='wallet'
	 LEFT JOIN v3_marketplace.account_profiles p ON p.user_id=u.id
	 WHERE u.status='active' AND u.deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("catalog: account profiles: %w", err)
	}
	profiles := make(map[int64]AccountProfile)
	for rows.Next() {
		var p AccountProfile
		var expiry *time.Time
		var settings []byte
		if err = rows.Scan(&p.UserID, &p.WalletAccountID, &p.MultiplierPPM, &expiry, &p.Cards, &p.AllowedGroups, &p.AutoGroups, &settings); err != nil {
			rows.Close()
			return nil, err
		}
		if err = readProfilePreference(&p, settings); err != nil {
			rows.Close()
			return nil, fmt.Errorf("catalog: user %d funding preference: %w", p.UserID, err)
		}
		if expiry != nil {
			p.MultiplierExpiresAt = *expiry
		}
		profiles[p.UserID] = p
	}
	rows.Close()
	return profiles, rows.Err()
}

// applyKeyBudgetAccounts attaches per-API-key budget accounts onto the
// matching user's profile.
func applyKeyBudgetAccounts(ctx context.Context, tx pgx.Tx, profiles map[int64]AccountProfile) error {
	rows, err := tx.Query(ctx, `SELECT k.user_id,k.id,a.id FROM v3_identity.api_keys k JOIN v3_billing.accounts a ON a.owner_type='api_key' AND a.owner_id=k.id AND a.kind='key_budget' WHERE k.status='active' AND k.deleted_at IS NULL`)
	if err != nil {
		return fmt.Errorf("catalog: key budget profiles: %w", err)
	}
	for rows.Next() {
		var user, key, account int64
		if err = rows.Scan(&user, &key, &account); err != nil {
			rows.Close()
			return err
		}
		if p, ok := profiles[user]; ok {
			if p.KeyBudgetAccounts == nil {
				p.KeyBudgetAccounts = map[int64]int64{}
			}
			p.KeyBudgetAccounts[key] = account
			profiles[user] = p
		}
	}
	rows.Close()
	return rows.Err()
}

// applySubscriptionBuckets attaches active subscription buckets onto the
// matching user's profile, ordered by expiry then id.
func applySubscriptionBuckets(ctx context.Context, tx pgx.Tx, profiles map[int64]AccountProfile) error {
	rows, err := tx.Query(ctx, `SELECT s.user_id,s.account_id,s.starts_at,LEAST(s.expires_at,COALESCE(s.next_reset_at,s.expires_at)),s.id,
	 (EXISTS(SELECT 1 FROM v3_commerce.orders o WHERE o.id=s.order_id AND o.state='paid') OR s.source='order'),s.model_limits,s.model_usage
	 FROM v3_commerce.subscriptions s WHERE s.state='active' AND s.account_id IS NOT NULL AND s.deleted_at IS NULL
	 AND NOT EXISTS(SELECT 1 FROM v3_commerce.package_checkouts pc WHERE pc.target_subscription_id=s.id AND pc.state IN ('preparing','checkout'))
	 AND NOT EXISTS(SELECT 1 FROM v3_commerce.subscription_operations op WHERE op.subscription_id=s.id AND op.kind IN ('conversion','invalidate','delete') AND op.state='pending')
	 ORDER BY s.expires_at,s.id`)
	if err != nil {
		return fmt.Errorf("catalog: profile subscriptions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var userID int64
		var b SubscriptionBucket
		if err := rows.Scan(&userID, &b.AccountID, &b.StartsAt, &b.ExpiresAt, &b.SubscriptionID, &b.Paid, &b.ModelLimits, &b.ModelUsage); err != nil {
			return err
		}
		p, ok := profiles[userID]
		if ok {
			p.Subscriptions = append(p.Subscriptions, b)
			profiles[userID] = p
		}
	}
	return rows.Err()
}

func loadAccountProfiles(ctx context.Context, tx pgx.Tx) (map[int64]AccountProfile, error) {
	profiles, err := loadBaseAccountProfiles(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := applyKeyBudgetAccounts(ctx, tx, profiles); err != nil {
		return nil, err
	}
	if err := applySubscriptionBuckets(ctx, tx, profiles); err != nil {
		return nil, err
	}
	return profiles, nil
}
