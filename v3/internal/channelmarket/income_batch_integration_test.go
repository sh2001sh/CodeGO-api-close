//go:build pgintegration

package channelmarket_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type checkedReleasePoster struct {
	base interface {
		PostTx(context.Context, pgx.Tx, billing.Entry) (billing.PostResult, error)
	}
	check   func() error
	checked bool
}

func (p *checkedReleasePoster) PostTx(ctx context.Context, tx pgx.Tx, e billing.Entry) (billing.PostResult, error) {
	if !p.checked {
		p.checked = true
		if err := p.check(); err != nil {
			return billing.PostResult{}, err
		}
	}
	return p.base.PostTx(ctx, tx, e)
}

func TestIncomeReleasePrelocksAllOwnersBeforeFirstTransfer(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	var otherWallet int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',3,'wallet') RETURNING id`).Scan(&otherWallet); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',1,'wallet')`); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []int64{1, 3} {
		if _, err := f.pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET owner_user_id=$2 WHERE channel_id=$1`, c.InternalChannelID, owner); err != nil {
			t.Fatal(err)
		}
		if err := pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
			return f.s.AccrueTx(ctx, tx, channelmarket.SettlementInput{RequestID: fmt.Sprintf("release-owner-%d", owner), ChannelID: c.InternalChannelID, ConsumerUserID: 2, ConsumerMicro: 1000, GrossMicro: 1000})
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.pool.Exec(ctx, `UPDATE v3_channelmarket.settlements SET available_at=$1`, time.Unix(f.now.Load(), 0).Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	poster := &checkedReleasePoster{base: f.poster, check: func() error {
		tx, err := f.pool.Begin(ctx)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `SELECT id FROM v3_billing.accounts WHERE id=$1 FOR UPDATE NOWAIT`, otherWallet)
		_ = tx.Rollback(ctx)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			return fmt.Errorf("later owner's lower-ID wallet not prelocked: %v", err)
		}
		return nil
	}}
	s := channelmarket.New(f.pool, nil, poster, channelmarket.Config{Now: func() time.Time { return time.Unix(f.now.Load(), 0) }}, nil)
	if result, err := s.ReleaseIncome(ctx, 100); err != nil || result.Count != 2 || result.Amount != 1900 {
		t.Fatalf("release=%+v err=%v", result, err)
	}
	for _, owner := range []int64{1, 3} {
		if f.balance(t, owner, "wallet") != 950 || f.balance(t, owner, "marketplace_pending") != 0 {
			t.Fatalf("owner %d financial totals differ", owner)
		}
	}
}

func TestIncomeBatchExactReplayRollbackAndFundingSemantics(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	input := []channelmarket.SettlementInput{
		{RequestID: "z-first", ChannelID: c.InternalChannelID, ConsumerUserID: 2, ConsumerMicro: 1009, GrossMicro: 1009, MultiplierPPM: 1000000},
		{RequestID: "a-second", ChannelID: c.InternalChannelID, ConsumerUserID: 2, ConsumerMicro: 19, GrossMicro: 19},
		{RequestID: "zero", ChannelID: c.InternalChannelID, ConsumerUserID: 2},
		{RequestID: "non-market", ChannelID: 999999, ConsumerUserID: 2, GrossMicro: 1000},
	}
	apply := func(in []channelmarket.SettlementInput) error {
		return pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error { return f.s.AccrueBatchTx(ctx, tx, in) })
	}
	if err := apply(append(input, input[0])); err != nil {
		t.Fatal(err)
	}
	if err := apply(input); err != nil {
		t.Fatal(err)
	}
	if got := f.balance(t, 1, "marketplace_pending"); got != 978 {
		t.Fatalf("held balance=%d, want 978", got)
	}
	var count, platform, version int64
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM v3_channelmarket.settlements`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("settlements=%d err=%v", count, err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT balance,version FROM v3_billing.accounts WHERE owner_type='platform'`).Scan(&platform, &version); err != nil || platform != 50 || version != 1 {
		t.Fatalf("commission=%d version=%d err=%v", platform, version, err)
	}
	rows, err := f.pool.Query(ctx, `SELECT l.request_id,l.balance_after,o.version FROM v3_billing.ledger_entries l JOIN v3_billing.balance_outbox o USING(operation_id) WHERE l.operation_id LIKE 'market-pending:%' ORDER BY o.id`)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []struct {
		request          string
		balance, version int64
	}{{"z-first", 959, 1}, {"a-second", 978, 2}} {
		var request string
		var balance, ver int64
		if !rows.Next() {
			t.Fatalf("missing outbox %d", i)
		}
		if err = rows.Scan(&request, &balance, &ver); err != nil || request != want.request || balance != want.balance || ver != want.version {
			t.Fatalf("outbox=%s/%d/%d err=%v", request, balance, ver, err)
		}
	}
	rows.Close()
	altered := input[0]
	altered.GrossMicro++
	fresh := input[0]
	fresh.RequestID = "rollback-fresh"
	if err = apply([]channelmarket.SettlementInput{fresh, altered}); !errors.Is(err, channelmarket.ErrConflict) {
		t.Fatalf("altered replay=%v", err)
	}
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM v3_channelmarket.settlements`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("partial replay committed=%d err=%v", count, err)
	}
	if err = apply([]channelmarket.SettlementInput{fresh, altered, input[0]}); !errors.Is(err, channelmarket.ErrConflict) {
		t.Fatalf("in-batch altered duplicate=%v", err)
	}
	fields := map[string]string{billing.FieldRequestID: "split-primary", billing.FieldModel: "fixture-model", billing.FieldChannelID: strconv.FormatInt(c.InternalChannelID, 10), billing.FieldUserID: "2", billing.FieldAmount: "5", "usage_total_amount": "1000", "funding_part": "primary", "marketplace_gross_micro": "1000", "marketplace_multiplier_ppm": "0", "billing_source": "subscription_wallet"}
	secondary := map[string]string{billing.FieldModel: "fixture-model", "funding_part": "secondary"}
	zero := map[string]string{billing.FieldModel: "fixture-model", billing.FieldChannelID: fields[billing.FieldChannelID], billing.FieldAmount: "0"}
	invalidOfficial := map[string]string{billing.FieldModel: "fixture-model", billing.FieldChannelID: "999999", billing.FieldAmount: "invalid"}
	err = pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
		return f.s.AccrueUsageBatchTx(ctx, tx, []map[string]string{fields, secondary, zero, invalidOfficial}, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	var consumer, factor int64
	var source string
	if err = f.pool.QueryRow(ctx, `SELECT consumer_micro,multiplier_ppm,billing_source FROM v3_channelmarket.settlements WHERE request_id='split-primary'`).Scan(&consumer, &factor, &source); err != nil || consumer != 1000 || factor != 0 || source != "subscription_wallet" {
		t.Fatalf("split=%d/%d/%s err=%v", consumer, factor, source, err)
	}
	fields["marketplace_gross_micro"] = "invalid"
	err = pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error { return f.s.AccrueUsageBatchTx(ctx, tx, []map[string]string{fields}, nil) })
	if !errors.Is(err, channelmarket.ErrInvalid) {
		t.Fatalf("invalid market fields=%v", err)
	}
}

func TestIncomeBatchConcurrentHotSellerConservesFunds(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	const workers, batch = 4, 80
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			input := make([]channelmarket.SettlementInput, batch)
			for i := range input {
				input[i] = channelmarket.SettlementInput{RequestID: fmt.Sprintf("hot-%d-%d", worker, i), ChannelID: c.InternalChannelID, ConsumerUserID: 2, ConsumerMicro: 1000, GrossMicro: 1000}
			}
			for range 2 {
				if err := pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error { return f.s.AccrueBatchTx(ctx, tx, input) }); err != nil {
					t.Error(err)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	var n, drift, ledgerRows, outboxRows int64
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM v3_channelmarket.settlements`).Scan(&n); err != nil || n != workers*batch {
		t.Fatalf("settlements=%d err=%v", n, err)
	}
	if got := f.balance(t, 1, "marketplace_pending"); got != workers*batch*950 {
		t.Fatalf("owner balance=%d", got)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.accounts a WHERE balance<>(SELECT coalesce(sum(amount),0) FROM v3_billing.ledger_entries WHERE account_id=a.id) OR version<>(SELECT count(*) FROM v3_billing.ledger_entries WHERE account_id=a.id)`).Scan(&drift); err != nil || drift != 0 {
		t.Fatalf("drift=%d err=%v", drift, err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_billing.ledger_entries),(SELECT count(*) FROM v3_billing.balance_outbox)`).Scan(&ledgerRows, &outboxRows); err != nil || ledgerRows != 2*workers*batch || outboxRows != ledgerRows {
		t.Fatalf("ledger/outbox=%d/%d err=%v", ledgerRows, outboxRows, err)
	}
}

func TestIncomeBatchPerformanceSameFixture(t *testing.T) {
	if os.Getenv("V3_TEST_BATCH_PERF") != "1" {
		t.Skip("set V3_TEST_BATCH_PERF=1 for measured old and batch accrual comparison")
	}
	f := setup(t)
	c := f.channel(t, "public")
	const batch = 500
	for round := range 3 {
		for _, mode := range []string{"single", "batch"} {
			input := make([]channelmarket.SettlementInput, batch)
			for i := range input {
				input[i] = channelmarket.SettlementInput{RequestID: fmt.Sprintf("perf-%d-%s-%d", round, mode, i), ChannelID: c.InternalChannelID, ConsumerUserID: 2, ConsumerMicro: credits.Micro(1000 + i%17), GrossMicro: credits.Micro(1000 + i%17), MultiplierPPM: 1000000}
			}
			start := time.Now()
			err := pgx.BeginFunc(context.Background(), f.pool, func(tx pgx.Tx) error {
				if mode == "batch" {
					return f.s.AccrueBatchTx(ctx, tx, input)
				}
				for _, p := range input {
					if err := f.s.AccrueTx(ctx, tx, p); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("round=%d mode=%s batch=%d elapsed_ms=%.3f events_per_second=%.1f", round, mode, batch, float64(time.Since(start).Microseconds())/1000, batch/time.Since(start).Seconds())
		}
	}
	var different, modes, total int64
	if err := f.pool.QueryRow(ctx, `WITH results AS(SELECT substring(request_id from 'perf-[0-9]+-([a-z]+)-') mode,sum(consumer_micro) consumer,sum(gross_micro) gross,sum(commission_micro) commission,sum(net_micro) net,count(*) n FROM v3_channelmarket.settlements GROUP BY 1)
	 SELECT (SELECT count(*) FROM results), (SELECT coalesce(sum(n),0) FROM results),
	 (SELECT count(*) FROM results a JOIN results b ON a.mode='single' AND b.mode='batch' WHERE (a.consumer,a.gross,a.commission,a.net,a.n) IS DISTINCT FROM (b.consumer,b.gross,b.commission,b.net,b.n))`).Scan(&modes, &total, &different); err != nil || different != 0 || modes != 2 || total != 6*batch {
		t.Fatalf("single vs batch mismatch=%d modes=%d total=%d err=%v", different, modes, total, err)
	}
}
