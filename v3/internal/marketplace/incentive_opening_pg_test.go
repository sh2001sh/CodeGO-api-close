//go:build pgintegration

package marketplace_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

var incentiveContext = context.Background()

type incentiveBoxFixture struct {
	s        *marketplace.Service
	pool     *pgxpool.Pool
	accounts *ledger.Accounts
}

func incentiveBoxes(t *testing.T) (*incentiveBoxFixture, marketplace.Pool) {
	t.Helper()
	pool, poster, accounts, cfg := marketplace.DependenciesForIntegrationTest(t)
	cfg.Draw = func(limit int64) (int64, error) { return limit - 1, nil }
	s := marketplace.New(pool, poster, accounts, nil, nil, cfg)
	p, err := s.SavePool(incentiveContext, marketplace.Pool{Name: "lucky standard", Scope: "standard", Enabled: true, Price: 100, DailyLimit: 100,
		Rewards: []marketplace.Reward{{Kind: "credits", Title: "ordinary", Weight: 1, Amount: 50}}})
	if err != nil {
		t.Fatal(err)
	}
	return &incentiveBoxFixture{s: s, pool: pool, accounts: accounts}, p
}

func (f *incentiveBoxFixture) count(t *testing.T, query string) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(incentiveContext, query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *incentiveBoxFixture) balance(t *testing.T, user int64) int64 {
	t.Helper()
	id, err := f.accounts.WalletAccount(incentiveContext, user)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := f.accounts.LedgerBalance(incentiveContext, id)
	if err != nil {
		t.Fatal(err)
	}
	return int64(b)
}

func TestStandardBoxCommitsOnceWithoutRetiredLuckyNumber(t *testing.T) {
	f, p := incentiveBoxes(t)
	if _, err := f.s.PurchaseBoxes(incentiveContext, 1, "lucky-purchase", p.ID, 1); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := f.s.OpenBoxes(incentiveContext, 1, "lucky-open", 1)
			if err != nil || len(v) != 1 {
				t.Errorf("concurrent lucky open: %+v err=%v", v, err)
			}
		}()
	}
	wg.Wait()
	var numbers, records, rewards int64
	if err := f.pool.QueryRow(incentiveContext, `SELECT
 (SELECT count(*) FROM v3_commerce.blind_box_daily_lucky_numbers),
 (SELECT count(*) FROM v3_marketplace.blind_box_open_records),
 (SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='reward')`).Scan(&numbers, &records, &rewards); err != nil {
		t.Fatal(err)
	}
	if numbers != 0 || records != 1 || rewards != 1 {
		t.Fatalf("atomic retired-number/open/reward: numbers=%d records=%d rewards=%d", numbers, records, rewards)
	}
	if b := f.balance(t, 1); b != 9950 {
		t.Fatalf("replay changed wallet: %d", b)
	}
}

func TestRetiredLuckyTableCannotBlockBoxAndReward(t *testing.T) {
	f, p := incentiveBoxes(t)
	if _, err := f.s.PurchaseBoxes(incentiveContext, 1, "lucky-failure-purchase", p.ID, 1); err != nil {
		t.Fatal(err)
	}
	// Failure in the retired number table must not affect inventory or money.
	if _, err := f.pool.Exec(incentiveContext, `ALTER TABLE v3_commerce.blind_box_daily_lucky_numbers ADD CONSTRAINT refuse_test_number CHECK(user_id<>1)`); err != nil {
		t.Fatal(err)
	}
	if v, err := f.s.OpenBoxes(incentiveContext, 1, "lucky-failure-open", 1); err != nil || len(v) != 1 {
		t.Fatalf("retired table blocked opening: %+v err=%v", v, err)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_open_records`); n != 1 {
		t.Fatalf("opening records=%d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='reward'`); n != 1 {
		t.Fatalf("rewards=%d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE status='available'`); n != 0 || f.balance(t, 1) != 9950 {
		t.Fatalf("opening inventory or wallet: available=%d balance=%d", n, f.balance(t, 1))
	}
	if v, err := f.s.OpenBoxes(incentiveContext, 1, "lucky-failure-open", 1); err != nil || len(v) != 1 {
		t.Fatalf("retry actual opening: %+v err=%v", v, err)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_commerce.blind_box_daily_lucky_numbers`); n != 0 || f.balance(t, 1) != 9950 {
		t.Fatalf("successful retry: numbers=%d balance=%d", n, f.balance(t, 1))
	}
}
