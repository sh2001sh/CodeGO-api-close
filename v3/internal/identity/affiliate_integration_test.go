//go:build pgintegration

package identity

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func affiliateFixture(t *testing.T) (*Control, *pgxpool.Pool, *redisx.Client, int64, int64) {
	t.Helper()
	pool, rdb := testDeps(t)
	seedKey(t, pool, 7, 11)
	var source, wallet int64
	if err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',7,'affiliate') RETURNING id`).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',7,'wallet') RETURNING id`).Scan(&wallet); err != nil {
		t.Fatal(err)
	}
	poster := ledger.NewPoster(pool)
	for id, amount := range map[int64]credits.Micro{source: 3000000, wallet: 10} {
		if _, err := poster.Post(ctx, billing.Entry{AccountID: id, Amount: amount, Kind: "opening", OperationID: "opening-" + strconv.FormatInt(id, 10)}); err != nil {
			t.Fatal(err)
		}
	}
	c, err := NewControl(pool, ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32), BudgetPoster: ledger.NewPoster(pool, rdb)}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	return c, pool, rdb, source, wallet
}

func TestAffiliateWithdrawalAtomicConcurrentAndReplay(t *testing.T) {
	c, pool, _, _, _ := affiliateFixture(t)
	in := AffiliateTransferInput{AmountMicroCredits: 1000000, OperationID: "one-withdrawal"}
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := c.TransferAffiliate(ctx, 7, in)
			if err == nil && (out.AffiliateMicroCredits != 2000000 || out.WalletMicroCredits != 1000010) {
				err = errors.New("replay returned a different monetary receipt")
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.TransferAffiliate(ctx, 7, AffiliateTransferInput{AmountMicroCredits: 2000000, OperationID: in.OperationID}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("changed replay admitted: %v", err)
	}
	seedKey(t, pool, 8, 12)
	if _, err := c.TransferAffiliate(ctx, 8, in); !errors.Is(err, ErrAffiliateFunds) {
		t.Fatalf("other user retrieved or spent receipt: %v", err)
	}
	results = make(chan error, 2)
	for _, op := range []string{"remaining-a", "remaining-b"} {
		wg.Add(1)
		go func(op string) {
			defer wg.Done()
			_, err := c.TransferAffiliate(ctx, 7, AffiliateTransferInput{AmountMicroCredits: 2000000, OperationID: op})
			results <- err
		}(op)
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrAffiliateFunds) {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='transfer'`).Scan(&count); err != nil || count != 4 || winners != 1 {
		t.Fatalf("transfer duplicated or overspent: entries=%d winners=%d err=%v", count, winners, err)
	}
	u, err := c.User(ctx, 7)
	if err != nil || u.AffiliateMicroCredits != 0 {
		t.Fatalf("DTO uses stale affiliate funds: %+v %v", u, err)
	}
	mustExec(t, pool, `UPDATE v3_identity.users SET status='disabled' WHERE id=7`)
	if _, err = c.TransferAffiliate(ctx, 7, in); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled user replay admitted: %v", err)
	}
}

type affiliateCreditFailure struct{ poster KeyBudgetPoster }

func (p affiliateCreditFailure) PostTx(ctx context.Context, tx pgx.Tx, e billing.Entry) (billing.PostResult, error) {
	if e.Amount > 0 {
		return billing.PostResult{}, errors.New("injected credit failure")
	}
	return p.poster.PostTx(ctx, tx, e)
}

func TestAffiliateWithdrawalFailureRollsBackAndReleasesReservation(t *testing.T) {
	c, pool, rdb, source, wallet := affiliateFixture(t)
	c.cfg.BudgetPoster = affiliateCreditFailure{poster: c.cfg.BudgetPoster}
	if _, err := c.TransferAffiliate(ctx, 7, AffiliateTransferInput{AmountMicroCredits: 1000000, OperationID: "fail-credit"}); err == nil {
		t.Fatal("second-leg failure accepted")
	}
	var sourceBalance, walletBalance, entries int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT balance FROM v3_billing.accounts WHERE id=$1),(SELECT balance FROM v3_billing.accounts WHERE id=$2),(SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='transfer')`, source, wallet).Scan(&sourceBalance, &walletBalance, &entries); err != nil || sourceBalance != 3000000 || walletBalance != 10 || entries != 0 {
		t.Fatalf("partial monetary commit: %d/%d entries=%d err=%v", sourceBalance, walletBalance, entries, err)
	}
	if n, err := billing.SweepPostingHolds(ctx, rdb, ledger.NewAccounts(pool), time.Now().Add(time.Hour), 100); err != nil || n != 1 {
		t.Fatalf("aborted debit hold not released: %d %v", n, err)
	}
	c.cfg.BudgetPoster = ledger.NewPoster(pool, rdb)
	mustExec(t, pool, `UPDATE v3_billing.accounts SET balance=$1 WHERE id=$2`, math.MaxInt64, wallet)
	if _, err := c.TransferAffiliate(ctx, 7, AffiliateTransferInput{AmountMicroCredits: 1000000, OperationID: "overflow"}); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("wallet overflow admitted: %v", err)
	}
	if err := rdb.HSet(ctx, billing.BalanceKey(source), "balance", "1000000", "reserved", "0", "ver", "1", "base", "1").Err(); err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, `UPDATE v3_billing.accounts SET balance=10 WHERE id=$1`, wallet)
	if _, err := c.TransferAffiliate(ctx, 7, AffiliateTransferInput{AmountMicroCredits: 2000000, OperationID: "hot-insufficient"}); !errors.Is(err, gateway.ErrInsufficientCredits) {
		t.Fatalf("Redis funds boundary bypassed: %v", err)
	}
	if err := rdb.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TransferAffiliate(ctx, 7, AffiliateTransferInput{AmountMicroCredits: 1000000, OperationID: "redis-down"}); !errors.Is(err, gateway.ErrBillingUnavailable) {
		t.Fatalf("closed funds guard bypassed: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_identity.affiliate_transfers`).Scan(&entries); err != nil || entries != 0 {
		t.Fatalf("failed withdrawals committed receipt: %d %v", entries, err)
	}
}
