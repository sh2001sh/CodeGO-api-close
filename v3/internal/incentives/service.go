// Package incentives owns historical lucky reward recovery, reset promises and paid-consumption referral rewards.
package incentives

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

var (
	ErrInvalid          = errors.New("incentives: invalid request")
	ErrNotFound         = errors.New("incentives: not found")
	ErrUnavailable      = errors.New("incentives: reset opportunities unavailable")
	ErrMonthlyUsed      = errors.New("incentives: reset already used this month")
	ErrReferralConflict = errors.New("incentives: referral terms or budget changed")
	ErrRetired          = errors.New("incentives: daily lucky number retired")
)

type Poster interface {
	PostTx(context.Context, pgx.Tx, billing.Entry) (billing.PostResult, error)
}
type Config struct {
	Now                 func() time.Time
	ResetSubscriptionTx func(context.Context, pgx.Tx, int64, string) error
}
type Service struct {
	pool   *pgxpool.Pool
	poster Poster
	cfg    Config
}

func New(pool *pgxpool.Pool, poster Poster, cfg Config) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{pool: pool, poster: poster, cfg: cfg}
}
func (s *Service) now() time.Time       { return s.cfg.Now() }
func shanghai() (*time.Location, error) { return time.LoadLocation("Asia/Shanghai") }

type Purchase struct {
	UserID, OrderID, PlanID, AmountMinor int64
	SourceType, SourceID                 string
}
type ResetSummary struct {
	AvailableCount int64  `json:"available_count"`
	EarnedTotal    int64  `json:"earned_total"`
	UsedTotal      int64  `json:"used_total"`
	ExchangedTotal int64  `json:"exchanged_total"`
	LastUsedMonth  string `json:"last_used_month"`
	CurrentMonth   string `json:"current_month"`
	UsedThisMonth  bool   `json:"used_this_month"`
}
