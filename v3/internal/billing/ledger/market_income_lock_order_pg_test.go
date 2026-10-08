//go:build pgintegration

package ledger

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestMarketBatchSellerSpendingAndIncomeReleaseUseGlobalAccountOrder(t *testing.T) {
	pool := testPool(t)
	wallet := fundedAccount(t, pool, 7, 10000)
	var secondaryAccount int64
	if err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind,balance) VALUES('api_key',70,'key_budget',10000) RETURNING id`).Scan(&secondaryAccount); err != nil {
		t.Fatal(err)
	}
	market := marketBatchFixture(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET owner_user_id=7 WHERE channel_id=3; UPDATE v3_catalog.channels SET owner_user_id=7 WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return market.AccrueTx(ctx, tx, channelmarket.SettlementInput{RequestID: "mature-owner-income", ChannelID: 3, ConsumerUserID: 7, ConsumerMicro: 1000, GrossMicro: 1000})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_channelmarket.settlements SET available_at=now()-interval '1 hour' WHERE request_id='mature-owner-income'`); err != nil {
		t.Fatal(err)
	}
	primary := marketBatchEvent(t, wallet, "seller-spends")
	primary.amount, primary.fields[billing.FieldAmount] = 700, "700"
	primary.fields["funding_part"], primary.fields["usage_total_amount"] = "primary", "1000"
	primary.fingerprint = fingerprint(primary.fields)
	secondary := primary
	secondary.accountID, secondary.amount = secondaryAccount, 300
	secondary.fields = make(map[string]string, len(primary.fields))
	for key, value := range primary.fields {
		secondary.fields[key] = value
	}
	secondary.fields[billing.FieldAccountID], secondary.fields[billing.FieldAmount], secondary.fields["funding_part"] = fmt.Sprint(secondaryAccount), "300", "secondary"
	secondary.fingerprint = fingerprint(secondary.fields)
	locked := make(chan int, 1)
	continueDebit := make(chan struct{})
	usageResult := make(chan error, 1)
	gate := func(ctx context.Context, tx pgx.Tx, _ map[string]string) error {
		var pid int
		if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		locked <- pid
		select {
		case <-continueDebit:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	workCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	go func() {
		_, err := postWithMarketplaceBatch(workCtx, pool, []event{primary, secondary}, nil, nil, market.AccrueUsageBatchTx, gate)
		usageResult <- err
	}()
	var usagePID int
	select {
	case usagePID = <-locked:
	case err := <-usageResult:
		t.Fatalf("usage failed before gate: %v", err)
	case <-workCtx.Done():
		t.Fatal(workCtx.Err())
	}
	// The secondary debit must be locked even though it is absent from the
	// primary events passed to the income hook.
	probe, err := pool.Begin(workCtx)
	if err != nil {
		t.Fatal(err)
	}
	_, lockErr := probe.Exec(workCtx, `SELECT id FROM v3_billing.accounts WHERE id=$1 FOR UPDATE NOWAIT`, secondaryAccount)
	_ = probe.Rollback(ctx)
	var pgErr *pgconn.PgError
	secondaryLocked := errors.As(lockErr, &pgErr) && pgErr.Code == "55P03"
	releaseResult := make(chan error, 1)
	go func() { _, err := market.ReleaseIncome(workCtx, 100); releaseResult <- err }()
	deadline := time.Now().Add(3 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		if err := pool.QueryRow(workCtx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, usagePID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(continueDebit)
	usageErr, releaseErr := <-usageResult, <-releaseResult
	if !waiting {
		t.Fatal("income release never reached the financial lock barrier")
	}
	if usageErr != nil || releaseErr != nil {
		t.Fatalf("seller spending / release lock inversion: usage=%v release=%v", usageErr, releaseErr)
	}
	if !secondaryLocked {
		t.Fatalf("secondary funding account was not prelocked: %v", lockErr)
	}
	if balance, version := pgBalance(t, pool, wallet); balance != 10250 || version != 2 {
		t.Fatalf("wallet=%d/%d", balance, version)
	}
	if balance, version := pgBalance(t, pool, secondaryAccount); balance != 9700 || version != 1 {
		t.Fatalf("secondary=%d/%d", balance, version)
	}
	var pending, version int64
	if err := pool.QueryRow(ctx, `SELECT balance,version FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='marketplace_pending'`).Scan(&pending, &version); err != nil || pending != 950 || version != 3 {
		t.Fatalf("held earnings=%d/%d err=%v", pending, version, err)
	}
	if count(t, pool, "ledger_entries") != 8 || count(t, pool, "balance_outbox") != 6 {
		t.Fatal("lost or duplicated transfer entries")
	}
}
