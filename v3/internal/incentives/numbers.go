package incentives

import (
	"context"
	"math/big"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func decimal(v string) (*big.Rat, error) {
	if len(v) > 256 {
		return nil, ErrInvalid
	}
	if at := strings.IndexAny(v, "eE"); at >= 0 {
		e, err := strconv.Atoi(v[at+1:])
		if err != nil || e < -128 || e > 128 {
			return nil, ErrInvalid
		}
	}
	r, ok := new(big.Rat).SetString(v)
	if !ok || r.Sign() < 0 {
		return nil, ErrInvalid
	}
	return r, nil
}
func rewardMicro(base, multiplier, jackpot string) (credits.Micro, error) {
	b, e := decimal(base)
	if e != nil {
		return 0, e
	}
	m, e := decimal(multiplier)
	if e != nil {
		return 0, e
	}
	j, e := decimal(jackpot)
	if e != nil {
		return 0, e
	}
	return rewardRatMicro(b, m, j)
}
func roundedCents(b *big.Rat) *big.Int {
	b = new(big.Rat).Mul(b, big.NewRat(100, 1))
	n := new(big.Int).Mul(b.Num(), big.NewInt(2))
	n.Add(n, b.Denom())
	n.Quo(n, new(big.Int).Mul(b.Denom(), big.NewInt(2)))
	return n
}
func rewardRatMicro(b, m, j *big.Rat) (credits.Micro, error) {
	n := roundedCents(new(big.Rat).Add(new(big.Rat).Mul(b, m), j))
	n.Mul(n, big.NewInt(10000))
	if !n.IsInt64() {
		return 0, credits.ErrOverflow
	}
	return credits.Micro(n.Int64()), nil
}

// Backfill rejects attempts to restart the retired daily lucky number feature.
func (s *Service) Backfill(context.Context) (int64, error) { return 0, ErrRetired }

// Assignment is retired even when an old caller still has the hook installed.
func (s *Service) AssignSubscriptionNumbersTx(context.Context, pgx.Tx, int64) error {
	return ErrRetired
}
func (s *Service) IssueBlindBoxNumberTx(context.Context, pgx.Tx, int64, int64) error {
	return ErrRetired
}
