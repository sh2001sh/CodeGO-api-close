//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestGroupCheckoutImportedPendingIntentRestoresExactRoomAndSkipsPaidHistory(t *testing.T) {
	f := newGroupFixture(t)
	ctx := context.Background()
	p := f.plan(t, 2)
	first, status, body := f.checkout(1, map[string]any{"plan_id": p.ID}, false)
	if status != 200 {
		t.Fatalf("founder=%d %s", status, body)
	}
	if err := f.pay(first); err != nil {
		t.Fatal(err)
	}
	id := f.room(t, first.ID)
	join, status, body := f.checkout(2, map[string]any{"plan_id": p.ID, "purchase_type": "join_group", "group_buy_id": id}, false)
	if status != 200 {
		t.Fatalf("join=%d %s", status, body)
	}
	// These are the target facts supplied by the offline source importer.
	if _, err := f.pool.Exec(ctx, `ALTER TABLE v3_commerce.orders ADD COLUMN IF NOT EXISTS group_buy_id bigint NOT NULL DEFAULT 0`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE v3_commerce.orders SET group_buy_id=$2 WHERE id=$1`, join.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM v3_commerce.group_checkouts`); err != nil {
		t.Fatal(err)
	}
	if n, err := f.s.InitializeImportedGroupCheckouts(ctx); err != nil || n != 1 {
		t.Fatalf("pending exact room import count=%d error=%v", n, err)
	}
	if n, err := f.s.InitializeImportedGroupCheckouts(ctx); err != nil || n != 0 {
		t.Fatalf("import replay not idempotent count=%d error=%v", n, err)
	}
	if err := f.pay(join); err != nil {
		t.Fatal(err)
	}
	if err := f.pay(first); err != nil {
		t.Fatal(err)
	}
	if f.room(t, join.ID) != id {
		t.Fatal("import changed chosen room")
	}
	var historicalIntent, members int
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_commerce.group_checkouts WHERE order_id=$1),(SELECT count(*) FROM v3_marketplace.group_buy_members)`, first.ID).Scan(&historicalIntent, &members); err != nil || historicalIntent != 0 || members != 2 {
		t.Fatalf("import reenrolled historical paid member: intent=%d members=%d error=%v", historicalIntent, members, err)
	}
}

func TestGroupCheckoutImportedJoinWithoutSourceRoomFailsWithoutGuessing(t *testing.T) {
	f := newGroupFixture(t)
	p := f.plan(t, 2)
	first, status, body := f.checkout(1, map[string]any{"plan_id": p.ID}, false)
	if status != 200 {
		t.Fatalf("founder=%d %s", status, body)
	}
	if err := f.pay(first); err != nil {
		t.Fatal(err)
	}
	join, status, body := f.checkout(2, map[string]any{"plan_id": p.ID, "purchase_type": "join_group", "group_buy_id": f.room(t, first.ID)}, false)
	if status != 200 {
		t.Fatalf("join=%d %s", status, body)
	}
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM v3_commerce.group_checkouts WHERE order_id=$1`, join.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := f.s.InitializeImportedGroupCheckouts(context.Background()); !errors.Is(err, commerce.ErrInvalid) || n != 0 {
		t.Fatalf("missing source room guessed from available pool: count=%d error=%v", n, err)
	}
}
