//go:build pgintegration

package marketplace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/migrations"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type fixture struct {
	s        *Service
	pool     *pgxpool.Pool
	now      atomic.Int64
	poster   *ledger.Poster
	accounts *ledger.Accounts
}

var testContext = context.Background()

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	pool, err := pgxpool.New(testContext, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	rows, err := pool.Query(testContext, `SELECT nspname FROM pg_namespace WHERE left(nspname,3)='v3_'`)
	if err != nil {
		t.Fatal(err)
	}
	var schemas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		schemas = append(schemas, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range schemas {
		if _, err := pool.Exec(testContext, "DROP SCHEMA "+pgx.Identifier{name}.Sanitize()+" CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	files, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		sql, err := migrations.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(testContext, sql); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	f := &fixture{pool: pool, poster: ledger.NewPoster(pool), accounts: ledger.NewAccounts(pool)}
	f.now.Store(time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC).Unix())
	for id := int64(1); id <= 3; id++ {
		if _, err := pool.Exec(testContext, `INSERT INTO v3_identity.users(id,username) VALUES($1,$2)`, id, fmt.Sprintf("user%d", id)); err != nil {
			t.Fatal(err)
		}
		account, err := f.accounts.WalletAccount(testContext, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.poster.Post(testContext, billing.Entry{AccountID: account, Amount: 10000, Kind: "opening", OperationID: fmt.Sprintf("opening:%d", id)}); err != nil {
			t.Fatal(err)
		}
	}
	f.s = New(pool, f.poster, f.accounts, testPurchases{}, nil, Config{Now: func() time.Time { return time.Unix(f.now.Load(), 0) }, Draw: func(int64) (int64, error) { return 0, nil }})
	return f
}

func (f *fixture) seedPool(t *testing.T, reward Reward) Pool {
	t.Helper()
	reward.Weight = 1
	if reward.Title == "" {
		reward.Title = "reward"
	}
	p, err := f.s.SavePool(testContext, Pool{Name: "test", Enabled: true, Price: 100, DailyLimit: 100, Rewards: []Reward{reward}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func (f *fixture) balance(t *testing.T, user int64) int64 {
	t.Helper()
	account, err := f.accounts.WalletAccount(testContext, user)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := f.accounts.LedgerBalance(testContext, account)
	if err != nil {
		t.Fatal(err)
	}
	return int64(b)
}
func (f *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(testContext, query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type testPurchases struct{}

func (testPurchases) PrepareGroupBonusTx(ctx context.Context, tx pgx.Tx, user, subscription int64, amount credits.Micro) (int64, error) {
	if subscription != user || amount <= 0 {
		return 0, ErrInvalidInput
	}
	var account int64
	err := tx.QueryRow(ctx, `SELECT id FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=$1 AND kind='wallet'`, user).Scan(&account)
	return account, err
}

func (testPurchases) GroupPurchaseTx(ctx context.Context, tx pgx.Tx, user, order int64) (GroupPurchase, error) {
	if order != user*100 {
		return GroupPurchase{}, ErrNotFound
	}
	var account int64
	if err := tx.QueryRow(ctx, `SELECT id FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=$1 AND kind='wallet'`, user).Scan(&account); err != nil {
		return GroupPurchase{}, err
	}
	return GroupPurchase{OrderID: order, PlanID: 1, SubscriptionID: user, TargetCount: 2, BonusMicro: 500, BonusAccountID: account, Lifetime: time.Hour, Enabled: true}, nil
}

type failRewards struct{ base Poster }

func (p failRewards) PostTx(ctx context.Context, tx pgx.Tx, e billing.Entry) (billing.PostResult, error) {
	if e.Kind == "reward" {
		return billing.PostResult{}, errors.New("injected ledger failure")
	}
	return p.base.PostTx(ctx, tx, e)
}

// External tests import commerce (which imports this package), so export only
// the disposable fixture dependencies from the test build to avoid an import cycle.
func DependenciesForIntegrationTest(t *testing.T) (*pgxpool.Pool, *ledger.Poster, *ledger.Accounts, Config) {
	f := newFixture(t)
	return f.pool, f.poster, f.accounts, f.s.cfg
}
