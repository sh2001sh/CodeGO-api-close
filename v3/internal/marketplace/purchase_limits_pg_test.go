//go:build pgintegration

package marketplace

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func purchaseLimitCheck(t *testing.T, f *fixture, p Pool, count int) error {
	t.Helper()
	return pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		if err := lockUser(testContext, tx, 1); err != nil {
			return err
		}
		return f.s.checkPurchaseLimitsTx(testContext, tx, 1, p, count)
	})
}

func TestPurchaseLimitsCashAndWalletConcurrentAdmission(t *testing.T) {
	for _, kind := range []string{"daily", "monthly"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
			p.DailyLimit = 1
			if kind == "monthly" {
				p.DailyLimit, p.MonthlyLimit = 100, 1
			}
			if _, err := f.s.SavePool(testContext, p); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				_, err := f.s.PurchaseBoxes(testContext, 1, "wallet-race", p.ID, 1)
				results <- err
			}()
			go func() {
				defer wg.Done()
				<-start
				results <- pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
					_, err := f.s.CreateBoxOrderTx(testContext, tx, BoxOrderInput{UserID: 1, PoolID: p.ID,
						Quantity: 1, AmountMinor: 250, TradeNo: "cash-race", Currency: "usd", PaymentProvider: "stripe"})
					return err
				})
			}()
			close(start)
			wg.Wait()
			close(results)
			var admitted, refused int
			limitErr := ErrDailyLimit
			if kind == "monthly" {
				limitErr = ErrMonthlyLimit
			}
			for err := range results {
				switch {
				case err == nil:
					admitted++
				case errors.Is(err, limitErr):
					refused++
				default:
					t.Fatalf("unexpected admission error: %v", err)
				}
			}
			if admitted != 1 || refused != 1 {
				t.Fatalf("two checkouts exceeded %s limit: admitted=%d refused=%d", kind, admitted, refused)
			}
		})
	}
}

func TestPurchaseLimitsDeduplicateExternalInventoryAndIgnoreGrantsRefunds(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	o := boxOrderFixture(t, f, p.ID, "cash-counted", 2)
	if err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		_, err := f.s.CompleteBoxOrderTx(testContext, tx, 1, o.TradeNo, o.AmountMinor, "usd")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PurchaseBoxes(testContext, 1, "wallet-counted", p.ID, 1); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"cancelled", "refunded", "failed", "expired"} {
		if _, err := f.pool.Exec(testContext, `INSERT INTO v3_marketplace.blind_box_orders
 (user_id,pool_id,quantity,trade_no,status,source,created_at) VALUES(1,$1,50,$2,$3,'purchase',$4)`, p.ID, "ignore-"+status, status, f.s.cfg.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.pool.Exec(testContext, `INSERT INTO v3_marketplace.blind_box_orders
 (user_id,pool_id,quantity,trade_no,status,source,created_at) VALUES(1,$1,50,'ignore-benefit','completed','subscription_benefit',$2)`, p.ID, f.s.cfg.Now()); err != nil {
		t.Fatal(err)
	}
	for _, excluded := range []struct {
		request, status string
		grant           bool
	}{{"ignored-grant", "completed", true}, {"ignored-revoked", "revoked", false}} {
		if _, err := f.pool.Exec(testContext, `INSERT INTO v3_marketplace.blind_box_purchases
 (user_id,pool_id,quantity,unit_price_micro,purchase_date,request_id,is_grant,status)
 VALUES(1,$1,50,0,$2,$3,$4,$5)`, p.ID, f.s.cfg.Now().In(f.s.cfg.Location).Format("2006-01-02"), excluded.request, excluded.grant, excluded.status); err != nil {
			t.Fatal(err)
		}
	}
	p.DailyLimit = 3
	if err := purchaseLimitCheck(t, f, p, 1); !errors.Is(err, ErrDailyLimit) {
		t.Fatalf("three paid purchases not counted: %v", err)
	}
	p.DailyLimit, p.MonthlyLimit = 4, 3
	if err := purchaseLimitCheck(t, f, p, 1); !errors.Is(err, ErrMonthlyLimit) {
		t.Fatalf("monthly paid purchases not counted: %v", err)
	}
	p.MonthlyLimit = 4
	if err := purchaseLimitCheck(t, f, p, 1); err != nil {
		t.Fatalf("external marker double counted or grants/refunds counted: %v", err)
	}
	if err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		return f.s.CancelBoxOrderTx(testContext, tx, 1, o.TradeNo)
	}); err != nil {
		t.Fatal(err)
	}
	p.DailyLimit, p.MonthlyLimit = 2, 2
	if err := purchaseLimitCheck(t, f, p, 1); err != nil {
		t.Fatalf("refund kept consuming limits: %v", err)
	}
}

func TestPurchaseLimitsShanghaiMidnightAndMonthBoundary(t *testing.T) {
	for _, date := range []time.Time{
		time.Date(2026, 9, 29, 15, 59, 59, 0, time.UTC),
		time.Date(2026, 9, 30, 15, 59, 59, 0, time.UTC),
	} {
		t.Run(date.Format("2006-01-02"), func(t *testing.T) {
			f := newFixture(t)
			f.now.Store(date.Unix())
			p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
			_ = boxOrderFixture(t, f, p.ID, "cash-boundary", 1)
			if _, err := f.s.PurchaseBoxes(testContext, 1, "wallet-boundary", p.ID, 1); err != nil {
				t.Fatal(err)
			}
			p.DailyLimit, p.MonthlyLimit = 2, 2
			if err := purchaseLimitCheck(t, f, p, 1); !errors.Is(err, ErrDailyLimit) {
				t.Fatalf("pre-midnight day not full: %v", err)
			}
			f.now.Add(1)
			err := purchaseLimitCheck(t, f, p, 1)
			if date.Day() == 29 && !errors.Is(err, ErrMonthlyLimit) {
				t.Fatalf("daily reset lost same-month usage: %v", err)
			}
			if date.Day() == 30 && err != nil {
				t.Fatalf("Shanghai month boundary failed to reset: %v", err)
			}
		})
	}
}

func TestOpenLimitsPerPoolIncludeAllDrawsAndShanghaiBoundary(t *testing.T) {
	f := newFixture(t)
	f.now.Store(time.Date(2026, 9, 30, 15, 59, 59, 0, time.UTC).Unix())
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	p.DailyOpenLimit = 2
	if _, err := f.s.SavePool(testContext, p); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := f.pool.Exec(testContext, `INSERT INTO v3_marketplace.blind_box_open_records
 (user_id,pool_id,created_at,request_id,reward) VALUES(1,$1,$2,$3,'{}')`, p.ID, f.s.cfg.Now(), fmt.Sprintf("retained-grant-open:%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	check := func(pool int64, count int) error {
		return pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
			if err := lockUser(testContext, tx, 1); err != nil {
				return err
			}
			return f.s.checkOpenLimitsTx(testContext, tx, 1, map[int64]int{pool: count})
		})
	}
	if err := check(p.ID, 1); !errors.Is(err, ErrOpenLimit) {
		t.Fatalf("grant/retained opens did not count: %v", err)
	}
	other := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	if err := check(other.ID, 100); err != nil {
		t.Fatalf("unlimited different pool affected: %v", err)
	}
	f.now.Add(1)
	if err := check(p.ID, 2); err != nil {
		t.Fatalf("Shanghai open boundary did not reset: %v", err)
	}
	if err := check(p.ID, 3); !errors.Is(err, ErrOpenLimit) {
		t.Fatalf("selected quantity exceeded limit: %v", err)
	}
}

func TestOpenLimitsAPIReplayAndFailureAreAtomic(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	p.DailyOpenLimit = 2
	if _, err := f.s.SavePool(testContext, p); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(testContext, `UPDATE v3_identity.users SET role='admin' WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.GrantBoxes(testContext, 3, 1, "limited-grant", p.ID, 3); err != nil {
		t.Fatal(err)
	}
	first, err := f.s.OpenBoxes(testContext, 1, "first-two", 2)
	if err != nil || len(first) != 2 {
		t.Fatalf("allowed opens failed: %v %v", first, err)
	}
	replay, err := f.s.OpenBoxes(testContext, 1, "first-two", 2)
	if err != nil || len(replay) != 2 || replay[0].ID != first[0].ID {
		t.Fatalf("cap blocked committed replay: %v %v", replay, err)
	}
	_, err = f.s.OpenBoxes(testContext, 1, "over-open-limit", 1)
	if !errors.Is(err, ErrOpenLimit) {
		t.Fatalf("API did not apply daily open cap: %v", err)
	}
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_open_records`) != 2 ||
		f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE status='available'`) != 1 ||
		f.balance(t, 1) != 10002 {
		t.Fatal("refused open changed history, inventory or wallet")
	}
	f.now.Add(24 * 3600)
	if records, err := f.s.OpenBoxes(testContext, 1, "next-day", 1); err != nil || len(records) != 1 {
		t.Fatalf("next-day inventory could not open: %v %v", records, err)
	}
}
