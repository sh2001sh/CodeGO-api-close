//go:build pgintegration

package incentives

import (
	"context"
	"testing"
)

func TestRetiredLuckyRecoveryReportsFailedHistoricalRewardWithoutNewDraws(t *testing.T) {
	s, pool, _ := fixture(t)
	seedDraw(t, s, 3000000)
	s.poster = failedPoster{}
	ctx := context.Background()
	if err := s.RecoverLuckyRewards(ctx); err == nil {
		t.Fatal("failure was swallowed")
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscription_lucky_draws`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("failed recovery created new draws: count=%d err=%v", n, err)
	}
}
