//go:build pgintegration

package commerce_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

func TestTypedRedemptionBoxesGiveSealedInventoryOnceWithoutWallet(t *testing.T) {
	s, _, pool, _ := cashBoxFixture(t)
	ctx := context.Background()
	code, err := s.IssueTypedRedemption(ctx, commerce.IssueRedemptionInput{Name: "boxes", RedeemType: "blind_box", BlindBoxQuantity: 3})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan commerce.RedemptionResult, 12)
	fail := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); r, e := s.RedeemTyped(ctx, 1, code.Key); results <- r; fail <- e }()
	}
	wg.Wait()
	close(results)
	close(fail)
	for e := range fail {
		if e != nil {
			t.Fatal(e)
		}
	}
	var prior commerce.RedemptionResult
	for r := range results {
		if r.RedeemType != "blind_box" || r.BlindBoxQuantity != 3 || r.BlindBoxOrderID <= 0 || r.Credits != 0 {
			t.Fatalf("result=%+v", r)
		}
		if prior.BlindBoxOrderID != 0 && prior != r {
			t.Fatalf("replay changed %+v", r)
		}
		prior = r
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE status='available'`); n != 3 {
		t.Fatalf("inventory=%d", n)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_orders WHERE status='completed' AND source='redemption' AND amount_minor=0 AND opened_count=0`); n != 1 {
		t.Fatalf("orders=%d", n)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_billing.ledger_entries`); n != 0 {
		t.Fatalf("money entries=%d", n)
	}
}

type brokenRedemptionBoxes struct{ commerce.CashBoxMarket }

func (b brokenRedemptionBoxes) CompleteBoxOrderTx(context.Context, pgx.Tx, int64, string, int64, string) (marketplace.Purchase, error) {
	return marketplace.Purchase{}, commerce.ErrProviderUnavailable
}

func TestTypedRedemptionBoxFailureRollsBackOrderInventoryAndClaim(t *testing.T) {
	s, m, pool, _ := cashBoxFixture(t)
	ctx := context.Background()
	code, err := s.IssueTypedRedemption(ctx, commerce.IssueRedemptionInput{Name: "rollback", RedeemType: "blind_box", BlindBoxQuantity: 2})
	if err != nil {
		t.Fatal(err)
	}
	s.SetCashBoxMarket(brokenRedemptionBoxes{m})
	if _, err = s.RedeemTyped(ctx, 1, code.Key); !errors.Is(err, commerce.ErrProviderUnavailable) {
		t.Fatalf("error=%v", err)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_orders`); n != 0 {
		t.Fatalf("orders=%d", n)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_items`); n != 0 {
		t.Fatalf("inventory=%d", n)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.redemption_codes WHERE id=$1 AND state='active' AND claimed_by IS NULL`, code.ID); n != 1 {
		t.Fatal("failed claim consumed code")
	}
	s.SetCashBoxMarket(m)
	if _, err = s.RedeemTyped(ctx, 1, code.Key); err != nil {
		t.Fatal(err)
	}
}

func TestTypedRedemptionImportedBoxQuantityPreservesSourceGrantBeyondCheckoutLimit(t *testing.T) {
	s, _, pool, _ := cashBoxFixture(t)
	ctx := context.Background()
	key := "legacy-box-code-with-101-items"
	digest := sha256.Sum256([]byte(key))
	if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.redemption_codes(code_hash,name,credits,redeem_type,blind_box_quantity)
	 VALUES($1,'source preserved quantity',0,'blind_box',101)`, digest[:]); err != nil {
		t.Fatal(err)
	}
	r, err := s.RedeemTyped(ctx, 1, key)
	if err != nil || r.BlindBoxQuantity != 101 || r.BlindBoxOrderID <= 0 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE status='available'`); n != 101 {
		t.Fatalf("items=%d", n)
	}
	if _, err = s.RedeemTyped(ctx, 1, key); err != nil {
		t.Fatal(err)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_items`); n != 101 {
		t.Fatalf("replay items=%d", n)
	}
}
