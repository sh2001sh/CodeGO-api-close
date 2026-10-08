//go:build pgintegration

package marketplace

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestInventorySelectionPreservesOtherPoolsOwnershipAndReplay(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 10})
	q := f.seedPool(t, Reward{Kind: "credits", Amount: 20})
	if _, err := f.s.PurchaseBoxes(testContext, 1, "buy-first", p.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PurchaseBoxes(testContext, 1, "buy-second", q.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.OpenBoxesFromPool(testContext, 2, "unauthorized", 1, q.ID); !errors.Is(err, ErrInventory) {
		t.Fatalf("opened someone else's inventory: %v", err)
	}
	records, err := f.s.OpenBoxesFromPool(testContext, 1, "selected-pool", 1, q.ID)
	if err != nil || len(records) != 1 || records[0].Reward.Amount != 20 {
		t.Fatalf("specific pool selection: %+v %v", records, err)
	}
	if remaining := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE pool_id=$1 AND status='available'`, p.ID); remaining != 2 {
		t.Fatalf("selection consumed another pool: %d", remaining)
	}
	if _, err := f.s.OpenBoxesFromPool(testContext, 1, "selected-pool", 1, p.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed pool reused request ID: %v", err)
	}
	again, err := f.s.OpenBoxesFromPool(testContext, 1, "selected-pool", 1, q.ID)
	if err != nil || len(again) != 1 || again[0].ID != records[0].ID {
		t.Fatalf("selected retry rerolled: %+v %v", again, err)
	}
	ordinary, err := f.s.OpenBoxes(testContext, 1, "legacy-request", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, omitted := range []int64{0} {
		again, err := f.s.OpenBoxesFromPool(testContext, 1, "legacy-request", 1, omitted)
		if err != nil || len(again) != 1 || again[0].ID != ordinary[0].ID {
			t.Fatalf("old operation fingerprint changed for %d: %+v %v", omitted, again, err)
		}
	}
	if _, err := f.s.OpenBoxesFromPool(testContext, 1, "negative-pool", 1, -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative pool accepted: %v", err)
	}
}

func TestInventorySelectionDistinguishesStoredAndDynamicStock(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 10})
	if _, err := f.s.PurchaseBoxes(testContext, 1, "buy-mixed-stock", p.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_items SET draw_current_pool=false WHERE id=(SELECT max(id) FROM v3_marketplace.blind_box_items WHERE pool_id=$1)`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_pools SET enabled=false WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	stored, dynamic := false, true
	records, err := f.s.OpenBoxesFromInventory(testContext, 1, "stored-selection", 1, p.ID, &stored)
	if err != nil || len(records) != 1 || records[0].Reward.Amount != 10 {
		t.Fatalf("stored stock blocked by older dynamic stock: %+v %v", records, err)
	}
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE pool_id=$1 AND draw_current_pool AND status='available'`, p.ID) != 1 {
		t.Fatal("stored selection consumed dynamic inventory")
	}
	again, err := f.s.OpenBoxesFromInventory(testContext, 1, "stored-selection", 1, p.ID, &stored)
	if err != nil || len(again) != 1 || again[0].ID != records[0].ID {
		t.Fatalf("stored selection replay changed: %+v %v", again, err)
	}
	if _, err := f.s.OpenBoxesFromInventory(testContext, 1, "stored-selection", 1, p.ID, &dynamic); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed inventory rule reused request ID: %v", err)
	}
	if _, err := f.s.OpenBoxesFromInventory(testContext, 1, "missing-pool", 1, 0, &stored); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("rule selector without pool accepted: %v", err)
	}
}

func TestInventoryGroupsAndDefaultOpeningPreferEarliestExpiry(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 10})
	q := f.seedPool(t, Reward{Kind: "credits", Amount: 20})
	if _, err := f.s.PurchaseBoxes(testContext, 1, "buy-long-lived", p.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PurchaseBoxes(testContext, 1, "buy-soon-expiry", q.ID, 2); err != nil {
		t.Fatal(err)
	}
	soon := f.s.cfg.Now().Add(time.Hour)
	if _, err := f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_items SET expires_at=$2 WHERE pool_id=$1`, q.ID, soon); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_items SET draw_current_pool=false WHERE id=(SELECT min(id) FROM v3_marketplace.blind_box_items WHERE pool_id=$1)`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_items SET expires_at=$2 WHERE id=(SELECT max(id) FROM v3_marketplace.blind_box_items WHERE pool_id=$1)`, q.ID, f.s.cfg.Now()); err != nil {
		t.Fatal(err)
	}
	view, err := f.s.Overview(testContext, 1)
	if err != nil || view.AvailableCount != 3 || len(view.Inventory) != 3 {
		t.Fatalf("grouped inventory includes expired or hides stored pools: %+v %v", view, err)
	}
	if view.Inventory[0].PoolID != q.ID || view.Inventory[0].AvailableCount != 1 || view.Inventory[0].ExpiresAt == nil || !view.Inventory[0].ExpiresAt.Equal(soon) {
		t.Fatalf("earliest inventory group wrong: %+v", view.Inventory)
	}
	if view.Inventory[1].PoolID != p.ID || view.Inventory[1].DrawCurrentPool || !view.Inventory[2].DrawCurrentPool {
		t.Fatalf("dynamic/stored inventory merged: %+v", view.Inventory)
	}
	records, err := f.s.OpenBoxes(testContext, 1, "default-expiry", 1)
	if err != nil || len(records) != 1 || records[0].Reward.Amount != 20 {
		t.Fatalf("default opened indefinite stock first: %+v %v", records, err)
	}
	if _, err := f.s.OpenBoxesFromPool(testContext, 1, "only-expired", 1, q.ID); !errors.Is(err, ErrInventory) {
		t.Fatalf("expired selected pool opened: %v", err)
	}
	view, err = f.s.Overview(testContext, 2)
	if err != nil || view.AvailableCount != 0 || len(view.Inventory) != 0 {
		t.Fatalf("another user's inventory leaked: %+v %v", view, err)
	}
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil || string(wire["inventory"]) != "[]" {
		t.Fatalf("empty inventory response lost array shape: %s %v", data, err)
	}
}
