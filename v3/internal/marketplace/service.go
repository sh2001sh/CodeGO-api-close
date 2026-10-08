// Package marketplace owns group-buy bonuses and sealed blind-box inventory.
// Every inventory transition and money movement commits in one PG transaction.
package marketplace

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

var (
	ErrInvalidInput = errors.New("marketplace: invalid input")
	ErrConflict     = errors.New("marketplace: request or state conflict")
	ErrNotFound     = errors.New("marketplace: not found")
	ErrUnavailable  = errors.New("marketplace: feature unavailable")
	ErrInventory    = errors.New("marketplace: insufficient inventory")
	ErrDailyLimit   = errors.New("marketplace: daily purchase limit exceeded")
	ErrMonthlyLimit = errors.New("marketplace: monthly purchase limit exceeded")
	ErrOpenLimit    = errors.New("marketplace: daily open limit exceeded")
)

type Poster interface {
	PostTx(context.Context, pgx.Tx, billing.Entry) (billing.PostResult, error)
}
type Wallets interface {
	WalletAccount(context.Context, int64) (int64, error)
}

// GroupPurchase is supplied by commerce after validating a paid order belongs
// to the user. Bonus values are frozen with the group, never taken from a client.
type GroupPurchase struct {
	OrderID, PlanID, SubscriptionID             int64
	TargetCount                                 int
	BonusMicro                                  int64
	BonusAt2Micro, BonusAt3Micro, BonusAt5Micro int64
	BonusAccountID                              int64
	Lifetime                                    time.Duration
	Enabled                                     bool
}
type Purchases interface {
	GroupPurchaseTx(context.Context, pgx.Tx, int64, int64) (GroupPurchase, error)
}

// GroupBonusBudgets resolves the subscription's current account and expands
// its funding budgets before marketplace posts the bonus in the same Tx.
type GroupBonusBudgets interface {
	PrepareGroupBonusTx(context.Context, pgx.Tx, int64, int64, credits.Micro) (int64, error)
}
type Subscriptions interface {
	GrantRewardTx(context.Context, pgx.Tx, int64, int64, string) error
}

// FrozenSubscriptions retains the complete promise of a published batch.
type FrozenSubscriptions interface {
	FreezeRewardPlanTx(context.Context, pgx.Tx, int64) (json.RawMessage, error)
	GrantFrozenRewardTx(context.Context, pgx.Tx, int64, json.RawMessage, string) error
}

type Config struct {
	Now func() time.Time
	// Draw returns a value in [0, limit). The default uses crypto/rand.
	Draw     func(int64) (int64, error)
	Location *time.Location
}
type Service struct {
	pool          *pgxpool.Pool
	money         Poster
	wallets       Wallets
	purchases     Purchases
	subscriptions Subscriptions
	cfg           Config
}

func New(pool *pgxpool.Pool, money Poster, wallets Wallets, purchases Purchases, subscriptions Subscriptions, cfg Config) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Draw == nil {
		cfg.Draw = func(limit int64) (int64, error) {
			n, err := rand.Int(rand.Reader, big.NewInt(limit))
			if err != nil {
				return 0, err
			}
			return n.Int64(), nil
		}
	}
	if cfg.Location == nil {
		cfg.Location = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return &Service{pool: pool, money: money, wallets: wallets, purchases: purchases, subscriptions: subscriptions, cfg: cfg}
}

func validRequest(userID int64, requestID string, count int) error {
	if userID <= 0 || requestID == "" || len(requestID) > 64 || count < 1 || count > 100 {
		return ErrInvalidInput
	}
	return nil
}

func lockUser(ctx context.Context, tx pgx.Tx, userID int64) error {
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR NO KEY UPDATE`, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
