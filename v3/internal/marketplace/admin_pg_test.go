//go:build pgintegration

package marketplace

import (
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing"
)

func TestAdminGrantRevokeAndInsufficientFunds(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	if _, err := f.s.GrantBoxes(testContext, 2, 1, "grant", p.ID, 2); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nonadmin grant=%v", err)
	}
	if _, err := f.pool.Exec(testContext, `UPDATE v3_identity.users SET role='admin' WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	grant, err := f.s.GrantBoxes(testContext, 3, 1, "grant", p.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.s.GrantBoxes(testContext, 3, 1, "grant", p.ID, 2)
	if err != nil || replay.ID != grant.ID {
		t.Fatalf("grant replay=%v %v", replay, err)
	}
	if b := f.balance(t, 1); b != 10000 {
		t.Fatalf("grant debited user=%d", b)
	}
	ids, err := f.s.RevokeBoxes(testContext, 3, 1, "revoke", 1)
	if err != nil || len(ids) != 1 {
		t.Fatalf("revoke=%v %v", ids, err)
	}
	if _, err := f.s.RevokeBoxes(testContext, 3, 1, "revoke", 1); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE status='available'`); n != 1 {
		t.Fatalf("available=%d", n)
	}
	account, err := f.accounts.WalletAccount(testContext, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.poster.Post(testContext, billing.Entry{AccountID: account, Amount: -10000, Kind: "transfer", OperationID: "drain"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PurchaseBoxes(testContext, 1, "no-funds", p.ID, 1); err == nil {
		t.Fatal("purchase accepted without funds")
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_purchases WHERE NOT is_grant`); n != 0 {
		t.Fatalf("purchase persisted despite insufficient funds=%d", n)
	}
}

func TestPitySurvivesBatchReplayAndRollback(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	p.Guarantees = Guarantees{First: []Reward{{Kind: "credits", Title: "first", Weight: 1, Amount: 5}}, Small: []Reward{{Kind: "credits", Title: "small", Weight: 1, Amount: 10}}, SmallAfter: 3, SmallReset: 10}
	if _, err := f.s.SavePool(testContext, p); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PurchaseBoxes(testContext, 1, "purchase", p.ID, 3); err != nil {
		t.Fatal(err)
	}
	f.s.money = failRewards{f.poster}
	if _, err := f.s.OpenBoxes(testContext, 1, "batch", 3); err == nil {
		t.Fatal("expected reward failure")
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_pity`); n != 0 {
		t.Fatalf("pity persisted on rollback=%d", n)
	}
	f.s.money = f.poster
	records, err := f.s.OpenBoxes(testContext, 1, "batch", 3)
	if err != nil {
		t.Fatal(err)
	}
	if records[0].Guarantee != "first" || records[2].Guarantee != "small" {
		t.Fatalf("guarantees=%v", records)
	}
	if _, err := f.s.OpenBoxes(testContext, 1, "batch", 3); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `SELECT opened FROM v3_marketplace.blind_box_pity WHERE user_id=1`); n != 3 {
		t.Fatalf("replay advanced pity=%d", n)
	}
	if b := f.balance(t, 1); b != 9716 {
		t.Fatalf("guaranteed balance=%d", b)
	}
}

func TestSimulationDoesNotMutateAssets(t *testing.T) {
	f := newFixture(t)
	p := f.seedPool(t, Reward{Kind: "credits", Amount: 1})
	r, state, err := f.s.Simulate(testContext, p.ID, 3, PityState{})
	if err != nil || len(r) != 3 || state.Opened != 3 {
		t.Fatalf("simulate=%v %+v %v", r, state, err)
	}
	for _, table := range []string{"blind_box_purchases", "blind_box_items", "blind_box_open_records", "blind_box_pity"} {
		if n := f.count(t, `SELECT count(*) FROM v3_marketplace.`+table); n != 0 {
			t.Fatalf("simulation wrote %s=%d", table, n)
		}
	}
	if f.balance(t, 1) != 10000 {
		t.Fatal("simulation changed wallet")
	}
}
