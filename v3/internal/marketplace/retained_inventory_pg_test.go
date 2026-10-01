//go:build pgintegration

package marketplace

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestRetainedFrozenRewardsAndSeparatePityPools(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	q := f.seedPool(t, Reward{Kind: "credits", Amount: 2})
	if _, err := f.s.PurchaseBoxes(testContext, 1, "buy-p", p.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PurchaseBoxes(testContext, 1, "buy-q", q.ID, 1); err != nil {
		t.Fatal(err)
	}
	frozen, _ := json.Marshal(Reward{Kind: "credits", Title: "legacy frozen", Amount: 777})
	if _, err := f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_items SET frozen_reward=$1,draw_current_pool=false WHERE id=(SELECT min(id) FROM v3_marketplace.blind_box_items WHERE owner_user_id=1)`, frozen); err != nil {
		t.Fatal(err)
	}
	p.Rewards[0].Amount = 999
	if _, err := f.s.SavePool(testContext, p); err != nil {
		t.Fatal(err)
	}
	records, err := f.s.OpenBoxes(testContext, 1, "open", 3)
	if err != nil {
		t.Fatal(err)
	}
	if records[0].Reward.Amount != 777 || records[1].Reward.Amount != 999 || records[2].Reward.Amount != 2 {
		t.Fatalf("frozen/current mismatch: %+v", records)
	}
	o, err := f.s.Overview(testContext, 1)
	if err != nil {
		t.Fatal(err)
	}
	if o.PityStates[p.ID].Opened != 2 || o.PityStates[q.ID].Opened != 1 {
		t.Fatalf("pity mixed: %+v", o.PityStates)
	}
	if f.balance(t, 1) != 11478 {
		t.Fatal("frozen reward money not retained")
	}
}

func TestExpiredRetainedInventoryDoesNotBlockValidGiftOrOpen(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 100})
	if _, err := f.s.PurchaseBoxes(testContext, 1, "buy", p.ID, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_items SET expires_at=$1 WHERE id=(SELECT min(id) FROM v3_marketplace.blind_box_items WHERE owner_user_id=1)`, f.s.cfg.Now()); err != nil {
		t.Fatal(err)
	}
	o, err := f.s.Overview(testContext, 1)
	if err != nil {
		t.Fatal(err)
	}
	if o.AvailableCount != 2 {
		t.Fatal("expired stock counted")
	}
	ids, err := f.s.GiftBoxes(testContext, 1, 2, "gift", 1)
	if err != nil {
		t.Fatal(err)
	}
	if ids[0] != 2 {
		t.Fatal("gift chose expired inventory")
	}
	if _, err := f.s.OpenBoxes(testContext, 1, "open", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.OpenBoxes(testContext, 1, "expired", 1); !errors.Is(err, ErrInventory) {
		t.Fatal(err)
	}
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_gifts`) != 1 || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_gift_items`) != 1 {
		t.Fatal("typed gift not written")
	}
	if _, err := f.s.GiftBoxes(testContext, 1, 2, "gift", 1); err != nil {
		t.Fatal(err)
	}
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_gifts`) != 1 {
		t.Fatal("gift replay duplicated history")
	}
}

func TestHistoricalOpenWithoutItemRemainsReadable(t *testing.T) {
	f := newFixture(t)
	if _, err := f.pool.Exec(testContext, `INSERT INTO v3_marketplace.blind_box_open_records(user_id,request_id,reward,created_at) VALUES(1,'legacy', '{"kind":"credits","title":"retained","amount_micro":100}', $1)`, time.Unix(100, 0)); err != nil {
		t.Fatal(err)
	}
	h, err := f.s.History(testContext, 1, 0, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 1 || h[0].ItemID != 0 || h[0].Reward.Amount != 100 {
		t.Fatalf("history: %+v", h)
	}
}
