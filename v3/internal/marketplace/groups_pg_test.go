//go:build pgintegration

package marketplace

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestGroupCompletionConcurrencyAndRollback(t *testing.T) {
	f := newFixture(t)
	g, err := f.s.CreateGroup(testContext, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.s.CreateGroup(testContext, 1, 100)
	if err != nil || replay.ID != g.ID {
		t.Fatalf("create replay=%v %v", replay, err)
	}
	f.s.money = failRewards{f.poster}
	if _, err := f.s.JoinGroup(testContext, 2, g.ID, 200); err == nil {
		t.Fatal("expected bonus posting failure")
	}
	if n := f.count(t, `SELECT current_count FROM v3_marketplace.group_buys WHERE id=$1`, g.ID); n != 1 {
		t.Fatalf("failed join count=%d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM v3_marketplace.group_buy_members WHERE group_buy_id=$1`, g.ID); n != 1 {
		t.Fatalf("failed join members=%d", n)
	}
	f.s.money = f.poster
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := f.s.JoinGroup(testContext, 2, g.ID, 200)
			if err != nil || result.Status != "completed" {
				t.Errorf("join=%v %v", result, err)
			}
		}()
	}
	wg.Wait()
	if f.balance(t, 1) != 10500 || f.balance(t, 2) != 10500 {
		t.Fatal("incorrect group bonuses")
	}
	if n := f.count(t, `SELECT count(*) FROM v3_billing.ledger_entries WHERE reason='group_buy_bonus'`); n != 2 {
		t.Fatalf("bonus entries=%d", n)
	}
	if _, err := f.s.JoinGroup(testContext, 3, g.ID, 300); !errors.Is(err, ErrConflict) {
		t.Fatalf("full group join=%v", err)
	}
	if _, err := f.s.CreateGroup(testContext, 3, 100); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign order=%v", err)
	}
}

type tierPurchases struct{ testPurchases }

func (p tierPurchases) GroupPurchaseTx(ctx context.Context, tx pgx.Tx, user, order int64) (GroupPurchase, error) {
	r, err := p.testPurchases.GroupPurchaseTx(ctx, tx, user, order)
	r.TargetCount = 5
	r.BonusAt2Micro = 200
	r.BonusAt3Micro = 300
	r.BonusAt5Micro = 500
	return r, err
}
func TestExpiredGroupGrantsActualMemberTier(t *testing.T) {
	f := newFixture(t)
	f.s.purchases = tierPurchases{}
	g, err := f.s.CreateGroup(testContext, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.JoinGroup(testContext, 2, g.ID, 200); err != nil {
		t.Fatal(err)
	}
	if f.balance(t, 1) != 10000 {
		t.Fatal("bonus granted before room closes")
	}
	f.now.Add(3600)
	if count, err := f.s.ExpireGroups(testContext); err != nil || count != 1 {
		t.Fatalf("settle=%d %v", count, err)
	}
	if f.balance(t, 1) != 10200 || f.balance(t, 2) != 10200 {
		t.Fatal("two-member tier was not granted")
	}
	g, err = f.s.GetGroup(testContext, g.ID)
	if err != nil || g.Status != "completed" || g.CurrentCount != 2 {
		t.Fatalf("settled=%+v %v", g, err)
	}
	if count, err := f.s.ExpireGroups(testContext); err != nil || count != 0 {
		t.Fatalf("replay settle=%d %v", count, err)
	}
}

func TestGroupExpiresAtBoundaryWithoutBonus(t *testing.T) {
	f := newFixture(t)
	g, err := f.s.CreateGroup(testContext, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	f.now.Add(3600)
	if _, err := f.s.JoinGroup(testContext, 2, g.ID, 200); !errors.Is(err, ErrConflict) {
		t.Fatalf("expiry boundary=%v", err)
	}
	if n, err := f.s.ExpireGroups(testContext); err != nil || n != 1 {
		t.Fatalf("expire=%d %v", n, err)
	}
	if n, err := f.s.ExpireGroups(testContext); err != nil || n != 0 {
		t.Fatalf("repeat expire=%d %v", n, err)
	}
	if f.balance(t, 1) != 10000 {
		t.Fatal("incomplete group received bonus")
	}
}

func TestGroupTxSharesCallerRollbackAndReplay(t *testing.T) {
	f := newFixture(t)
	rollback := errors.New("caller payment failed")
	err := pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		if _, err := f.s.CreateGroupTx(testContext, tx, 1, 100); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) || f.count(t, `SELECT count(*) FROM v3_marketplace.group_buys`) != 0 {
		t.Fatalf("create did not roll back with caller: %v", err)
	}
	var group Group
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		var err error
		group, err = f.s.CreateGroupTx(testContext, tx, 1, 100)
		if err != nil {
			return err
		}
		replay, err := f.s.CreateGroupTx(testContext, tx, 1, 100)
		if err == nil && replay.ID != group.ID {
			return errors.New("create replay allocated second group")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		if _, err := f.s.JoinGroupTx(testContext, tx, 2, group.ID, 200); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) || f.balance(t, 1) != 10000 || f.balance(t, 2) != 10000 ||
		f.count(t, `SELECT count(*) FROM v3_marketplace.group_buy_members`) != 1 {
		t.Fatalf("join/bonus escaped caller rollback: %v", err)
	}
	err = pgx.BeginFunc(testContext, f.pool, func(tx pgx.Tx) error {
		g, err := f.s.JoinGroupTx(testContext, tx, 2, group.ID, 200)
		if err != nil {
			return err
		}
		replay, err := f.s.JoinGroupTx(testContext, tx, 2, group.ID, 200)
		if err == nil && (replay.ID != g.ID || replay.Status != "completed") {
			return errors.New("join replay diverged")
		}
		return err
	})
	if err != nil || f.balance(t, 1) != 10500 || f.balance(t, 2) != 10500 ||
		f.count(t, `SELECT count(*) FROM v3_billing.ledger_entries WHERE reason='group_buy_bonus'`) != 2 {
		t.Fatalf("committed group bonus/replay: %v", err)
	}
	if _, err := f.s.CreateGroupTx(testContext, nil, 1, 100); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil create tx=%v", err)
	}
	if _, err := f.s.JoinGroupTx(testContext, nil, 2, group.ID, 200); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil join tx=%v", err)
	}
}
