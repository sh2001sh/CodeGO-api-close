//go:build pgintegration

package commerce_test

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

type importedCashQueryKey struct{}

// Delay only the first callback's observations, so a replay really holds the
// financial row while that callback still owns the original cash-order row.
type importedCashLockTrace struct {
	missing, cashLocked  chan struct{}
	created, replayReady chan struct{}
	first, cash          sync.Once
}

func (tr *importedCashLockTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, importedCashQueryKey{}, data.SQL)
}

func (tr *importedCashLockTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	query, _ := ctx.Value(importedCashQueryKey{}).(string)
	if strings.Contains(query, "FROM v3_commerce.orders WHERE trade_no=") && data.CommandTag.RowsAffected() == 0 {
		tr.first.Do(func() {
			close(tr.missing)
			select {
			case <-tr.created:
			case <-ctx.Done():
			}
		})
	}
	if strings.Contains(query, "FROM v3_marketplace.blind_box_orders WHERE trade_no=") && strings.Contains(query, "FOR UPDATE") && data.Err == nil {
		tr.cash.Do(func() {
			close(tr.cashLocked)
			select {
			case <-tr.replayReady:
			case <-ctx.Done():
			}
		})
	}
}

func TestImportedCashBoxFirstCallbackAndReplayCannotInvertOrderLocks(t *testing.T) {
	s, _, pool, p := cashBoxFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_orders
	 (user_id,pool_id,trade_no,quantity,amount_minor,currency,payment_method,payment_provider,source,status)
	 VALUES(1,$1,'v2-lock-interleave',2,500,'cny','alipay','epay','purchase','expired')`, p.ID); err != nil {
		t.Fatal(err)
	}
	tr := &importedCashLockTrace{missing: make(chan struct{}), cashLocked: make(chan struct{}), created: make(chan struct{}), replayReady: make(chan struct{})}
	config := pool.Config()
	config.ConnConfig.Tracer = tr
	tracedPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tracedPool.Close)
	delayed := commerce.New(tracedPool, ledger.NewPoster(tracedPool), []commerce.PaymentProvider{
		commerce.NewEpay(commerce.EpayConfig{MerchantID: "merchant", Secret: "test-only-secret"})}, commerce.Config{})
	v := url.Values{"pid": {"merchant"}, "out_trade_no": {"v2-lock-interleave"}, "trade_no": {"interleaved-platform-id"}, "trade_status": {"TRADE_SUCCESS"}, "money": {"5.00"}, "sign_type": {"MD5"}}
	v.Set("sign", cashBoxSign(v))
	body := []byte(v.Encode())
	firstResult := make(chan error, 1)
	go func() { firstResult <- delayed.HandleWebhook(ctx, "epay", nil, body) }()
	select {
	case <-tr.missing:
	case <-ctx.Done():
		t.Fatal("first callback did not observe missing financial order")
	}
	if err = s.HandleWebhook(ctx, "epay", nil, body); err != nil {
		t.Fatal("other callback could not persist the original payment", err)
	}
	close(tr.created)
	select {
	case <-tr.cashLocked:
	case <-ctx.Done():
		t.Fatal("first callback did not acquire the original cash-order row")
	}
	replayResult := make(chan error, 1)
	go func() { replayResult <- s.HandleWebhook(ctx, "epay", nil, body) }()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
		 WHERE datname=current_database() AND wait_event_type='Lock'
		 AND query LIKE '%FROM v3_marketplace.blind_box_orders WHERE trade_no=%')`).Scan(&waiting)
		if err != nil {
			t.Fatal("cannot observe the real replay lock wait", err)
		}
		if waiting {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("replay did not reach the original cash-order lock")
		}
	}
	close(tr.replayReady)
	for name, result := range map[string]<-chan error{"first callback": firstResult, "replay": replayResult} {
		if err = <-result; err != nil {
			t.Errorf("%s failed under mixed first/replay locking: %v", name, err)
		}
	}
	for query, want := range map[string]int64{
		`SELECT count(*) FROM v3_commerce.orders`:                  1,
		`SELECT count(*) FROM v3_commerce.payment_events`:          1,
		`SELECT count(*) FROM v3_commerce.package_payment_reviews`: 1,
		`SELECT count(*) FROM v3_identity.notifications`:           1,
		`SELECT count(*) FROM v3_billing.ledger_entries`:           0,
		`SELECT count(*) FROM v3_marketplace.blind_box_items`:      0,
	} {
		if got := cashBoxCount(t, pool, query); got != want {
			t.Errorf("interleaved callback invariant: got=%d want=%d", got, want)
		}
	}
}
