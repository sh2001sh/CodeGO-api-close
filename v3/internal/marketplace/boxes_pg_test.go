//go:build pgintegration

package marketplace

import (
	"errors"
	"sync"
	"testing"
)

// Regression: duplicate HTTP retries must neither debit twice nor mint twice.
func TestPurchaseAndOpenConcurrentIdempotency(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 50})
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := f.s.PurchaseBoxes(testContext, 1, "purchase", p.ID, 5)
			if err != nil || r.Quantity != 5 {
				t.Errorf("purchase=%+v error=%v", r, err)
			}
		}()
	}
	wg.Wait()
	if b := f.balance(t, 1); b != 9500 {
		t.Fatalf("balance after purchase=%d", b)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items`); n != 5 {
		t.Fatalf("inventory=%d", n)
	}
	if _, err := f.s.PurchaseBoxes(testContext, 1, "purchase", p.ID, 6); !errors.Is(err, ErrConflict) {
		t.Fatalf("reused payload=%v", err)
	}
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := f.s.OpenBoxes(testContext, 1, "open", 1)
			if err != nil || len(r) != 1 {
				t.Errorf("open=%v error=%v", r, err)
			}
		}()
	}
	wg.Wait()
	if b := f.balance(t, 1); b != 9550 {
		t.Fatalf("balance after open=%d", b)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_open_records`); n != 1 {
		t.Fatalf("records=%d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE status='available'`); n != 4 {
		t.Fatalf("available=%d", n)
	}
}

func TestBoxFailureRollsBackInventoryAndMoney(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 50})
	if _, err := f.s.PurchaseBoxes(testContext, 1, "purchase", p.ID, 1); err != nil {
		t.Fatal(err)
	}
	f.s.money = failRewards{f.poster}
	if _, err := f.s.OpenBoxes(testContext, 1, "open", 1); err == nil {
		t.Fatal("expected ledger failure")
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE status='available'`); n != 1 {
		t.Fatalf("available=%d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_open_records`); n != 0 {
		t.Fatalf("records persisted after failure=%d", n)
	}
	f.s.money = f.poster
	if _, err := f.s.OpenBoxes(testContext, 1, "open", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.OpenBoxes(testContext, 1, "no-stock", 1); !errors.Is(err, ErrInventory) {
		t.Fatalf("empty inventory=%v", err)
	}
	if b := f.balance(t, 1); b != 9950 {
		t.Fatalf("final balance=%d", b)
	}
}

func TestPurchaseBoundariesAndGiftReplay(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "extra_draw"})
	p.DailyLimit = 2
	if _, err := f.s.SavePool(testContext, p); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PurchaseBoxes(testContext, 1, "purchase", p.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PurchaseBoxes(testContext, 1, "limit", p.ID, 1); !errors.Is(err, ErrDailyLimit) {
		t.Fatalf("daily limit=%v", err)
	}
	ids, err := f.s.GiftBoxes(testContext, 1, 2, "gift", 1)
	if err != nil || len(ids) != 1 {
		t.Fatalf("gift=%v %v", ids, err)
	}
	replay, err := f.s.GiftBoxes(testContext, 1, 2, "gift", 1)
	if err != nil || replay[0] != ids[0] {
		t.Fatalf("replay=%v %v", replay, err)
	}
	if _, err := f.s.GiftBoxes(testContext, 1, 3, "gift", 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("recipient conflict=%v", err)
	}
	if _, err := f.s.OpenBoxes(testContext, 2, "open", 1); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE owner_user_id=2 AND status='available'`); n != 1 {
		t.Fatalf("extra draw inventory=%d", n)
	}
	if b := f.balance(t, 2); b != 10000 {
		t.Fatalf("gift opening charged recipient=%d", b)
	}
}

func TestMultiplierPauseAndExpiryProjection(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "multiplier", MultiplierPPM: 100000, DurationSeconds: 900})
	if _, err := f.s.PurchaseBoxes(testContext, 1, "purchase", p.ID, 1); err != nil {
		t.Fatal(err)
	}
	r, err := f.s.OpenBoxes(testContext, 1, "open", 1)
	if err != nil {
		t.Fatal(err)
	}
	prop := r[0].PropID
	used, err := f.s.UseProp(testContext, 1, prop)
	if err != nil || used.Status != "active" {
		t.Fatalf("use=%+v %v", used, err)
	}
	if n := f.count(t, `SELECT multiplier_ppm FROM v3_marketplace.account_profiles WHERE user_id=1`); n != 100000 {
		t.Fatalf("factor=%d", n)
	}
	f.now.Add(300)
	paused, err := f.s.PauseProp(testContext, 1, prop)
	if err != nil || paused.RemainingSeconds != 600 {
		t.Fatalf("pause=%+v %v", paused, err)
	}
	if n := f.count(t, `SELECT multiplier_ppm FROM v3_marketplace.account_profiles WHERE user_id=1`); n != 1000000 {
		t.Fatalf("paused factor=%d", n)
	}
	if _, err := f.s.UseProp(testContext, 1, prop); err != nil {
		t.Fatal(err)
	}
	f.now.Add(600)
	if n, err := f.s.ExpireProps(testContext, 10); err != nil || n != 1 {
		t.Fatalf("expire=%d %v", n, err)
	}
	if _, err := f.s.UseProp(testContext, 1, prop); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired use=%v", err)
	}
	if n := f.count(t, `SELECT multiplier_ppm FROM v3_marketplace.account_profiles WHERE user_id=1`); n != 1000000 {
		t.Fatalf("expired factor=%d", n)
	}
}
