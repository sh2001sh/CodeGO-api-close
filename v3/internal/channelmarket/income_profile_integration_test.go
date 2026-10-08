//go:build pgintegration

package channelmarket_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestConcurrentIncomeAccrualDoesNotInvalidateAccountProfiles(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	if _, err := f.pool.Exec(ctx, `DELETE FROM v3_platform.cache_invalidation_outbox`); err != nil {
		t.Fatal(err)
	}
	const requests = 20
	var wg sync.WaitGroup
	errs := make(chan error, requests)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
				return f.s.AccrueTx(ctx, tx, channelmarket.SettlementInput{RequestID: fmt.Sprintf("profile-churn-%d", i), ChannelID: c.InternalChannelID, ConsumerUserID: 2, ConsumerMicro: 1000, GrossMicro: 1000, MultiplierPPM: 100000})
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var invalidations, platformAccounts, platformBalance int64
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_platform.cache_invalidation_outbox WHERE entity='account_profile'),count(*),coalesce(sum(balance),0) FROM v3_billing.accounts WHERE owner_type='platform' AND owner_id=1 AND kind='platform_revenue'`).Scan(&invalidations, &platformAccounts, &platformBalance); err != nil {
		t.Fatal(err)
	}
	if invalidations != 0 || platformAccounts != 1 || platformBalance != requests*50 || f.balance(t, 1, "marketplace_pending") != requests*950 {
		t.Fatalf("accrual profile churn or accounting mismatch: invalidations=%d platform accounts=%d balance=%d", invalidations, platformAccounts, platformBalance)
	}
}
