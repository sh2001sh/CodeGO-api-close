//go:build pgintegration

package commerce_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

func TestTypedRedemptionBoxesDisabledWithoutInventoryOrWalletMutation(t *testing.T) {
	s, _, pool, _ := cashBoxFixture(t)
	ctx := context.Background()
	if _, err := s.IssueTypedRedemption(ctx, commerce.IssueRedemptionInput{Name: "boxes", RedeemType: "blind_box", BlindBoxQuantity: 3}); !errors.Is(err, marketplace.ErrBlindBoxesDisabled) {
		t.Fatalf("new issuance=%v", err)
	}
	key := "historical-unclaimed-box-code"
	digest := sha256.Sum256([]byte(key))
	if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.redemption_codes(code_hash,name,credits,redeem_type,blind_box_quantity) VALUES($1,'historical unclaimed',0,'blind_box',3)`, digest[:]); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	fail := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.RedeemTyped(ctx, 1, key); fail <- e }()
	}
	wg.Wait()
	close(fail)
	for e := range fail {
		if !errors.Is(e, marketplace.ErrBlindBoxesDisabled) {
			t.Fatalf("historical unclaimed redemption=%v", e)
		}
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_items`); n != 0 {
		t.Fatalf("inventory=%d", n)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_orders`); n != 0 {
		t.Fatalf("orders=%d", n)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.redemption_codes WHERE code_hash=$1 AND state='active' AND claimed_by IS NULL AND redeem_result IS NULL`, digest[:]); n != 1 {
		t.Fatal("retired redemption consumed historical code")
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_billing.ledger_entries`); n != 0 {
		t.Fatalf("money entries=%d", n)
	}
}

func TestTypedRedemptionHistoricalConsumedBoxesReplayWithoutInventoryOrMoney(t *testing.T) {
	s, _, pool, _ := cashBoxFixture(t)
	ctx := context.Background()
	key := "historical-consumed-box-code"
	digest := sha256.Sum256([]byte(key))
	want := commerce.RedemptionResult{RedeemType: "blind_box", BlindBoxQuantity: 2, BlindBoxOrderID: 17}
	saved, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_commerce.redemption_codes(code_hash,name,credits,redeem_type,blind_box_quantity,state,claimed_by,claimed_at,redeem_result) VALUES($1,'historical consumed',0,'blind_box',2,'used',1,now(),$2)`, digest[:], saved); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan commerce.RedemptionResult, 12)
	fail := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); r, e := s.RedeemTyped(ctx, 1, key); results <- r; fail <- e }()
	}
	wg.Wait()
	close(results)
	close(fail)
	for err := range fail {
		if err != nil {
			t.Fatal(err)
		}
	}
	for result := range results {
		if result != want {
			t.Fatalf("historical receipt changed=%+v", result)
		}
	}
	if _, err = s.RedeemTyped(ctx, 2, key); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("historical receipt ownership=%v", err)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_orders`); n != 0 {
		t.Fatalf("orders=%d", n)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_items`); n != 0 {
		t.Fatalf("inventory=%d", n)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_billing.ledger_entries`); n != 0 {
		t.Fatalf("money entries=%d", n)
	}
}

func TestTypedRedemptionImportedBoxesRemainUnclaimedAndRevocable(t *testing.T) {
	s, _, pool, _ := cashBoxFixture(t)
	ctx := context.Background()
	key := "legacy-box-code-with-101-items"
	digest := sha256.Sum256([]byte(key))
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO v3_commerce.redemption_codes(code_hash,name,credits,redeem_type,blind_box_quantity)
	 VALUES($1,'source preserved quantity',0,'blind_box',101) RETURNING id`, digest[:]).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := s.RedeemTyped(ctx, 1, key); !errors.Is(err, marketplace.ErrBlindBoxesDisabled) {
			t.Fatalf("historical unclaimed redemption=%v", err)
		}
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.redemption_codes WHERE id=$1 AND blind_box_quantity=101 AND state='active' AND claimed_by IS NULL AND redeem_result IS NULL`, id); n != 1 {
		t.Fatal("historical code quantity or claim changed")
	}
	if err := s.RevokeRedemption(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemTyped(ctx, 1, key); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("revoked redemption=%v", err)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_items`); n != 0 {
		t.Fatalf("replay items=%d", n)
	}
}
