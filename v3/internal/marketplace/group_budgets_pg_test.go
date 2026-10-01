//go:build pgintegration

package marketplace

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type rotatedPurchases struct {
	testPurchases
	accounts map[int64]int64
	failUser int64
}

func (p rotatedPurchases) PrepareGroupBonusTx(_ context.Context, _ pgx.Tx, user, subscription int64, amount credits.Micro) (int64, error) {
	if user != subscription || amount != 500 {
		return 0, ErrInvalidInput
	}
	if user == p.failUser {
		return 0, ErrConflict
	}
	return p.accounts[user], nil
}

// Deliberately lacks GroupBonusBudgets so production cannot silently post a
// bonus without expanding the paid package's funding budgets.
type purchasesWithoutBudgets struct{}

func (purchasesWithoutBudgets) GroupPurchaseTx(ctx context.Context, tx pgx.Tx, user, order int64) (GroupPurchase, error) {
	return (testPurchases{}).GroupPurchaseTx(ctx, tx, user, order)
}

func TestGroupBonusUsesRotatedAccountAndRollsBackPreparation(t *testing.T) {
	f := newFixture(t)
	g, err := f.s.CreateGroup(testContext, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	oldAccount := f.count(t, `SELECT account_id FROM v3_marketplace.group_buy_members WHERE group_buy_id=$1`, g.ID)
	rotated := rotatedPurchases{accounts: make(map[int64]int64), failUser: 2}
	for user := int64(1); user <= 2; user++ {
		var id int64
		if err := f.pool.QueryRow(testContext, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('subscription',$1,'subscription') RETURNING id`, user).Scan(&id); err != nil {
			t.Fatal(err)
		}
		rotated.accounts[user] = id
	}
	f.s.purchases = rotated
	if _, err := f.s.JoinGroup(testContext, 2, g.ID, 200); !errors.Is(err, ErrConflict) {
		t.Fatalf("failed budget preparation: %v", err)
	}
	if f.count(t, `SELECT account_id FROM v3_marketplace.group_buy_members WHERE group_buy_id=$1`, g.ID) != oldAccount ||
		f.count(t, `SELECT count(*) FROM v3_billing.ledger_entries WHERE reason='group_buy_bonus'`) != 0 {
		t.Fatal("partial budget preparation escaped transaction rollback")
	}
	rotated.failUser = 0
	f.s.purchases = rotated
	if _, err := f.s.JoinGroup(testContext, 2, g.ID, 200); err != nil {
		t.Fatal(err)
	}
	for user, account := range rotated.accounts {
		balance, _, err := f.accounts.LedgerBalance(testContext, account)
		if err != nil || balance != 500 || f.balance(t, user) != 10000 ||
			f.count(t, `SELECT count(*) FROM v3_marketplace.group_buy_members WHERE group_buy_id=$1 AND user_id=$2 AND account_id=$3 AND bonus_granted`, g.ID, user, account) != 1 {
			t.Fatalf("bonus did not follow current account user%d: %d %v", user, balance, err)
		}
	}
}

func TestGroupBonusWithoutBudgetPortFailsClosed(t *testing.T) {
	f := newFixture(t)
	f.s.purchases = purchasesWithoutBudgets{}
	g, err := f.s.CreateGroup(testContext, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.JoinGroup(testContext, 2, g.ID, 200); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing budget adapter silently succeeded: %v", err)
	}
	if f.count(t, `SELECT current_count FROM v3_marketplace.group_buys WHERE id=$1`, g.ID) != 1 ||
		f.count(t, `SELECT count(*) FROM v3_marketplace.group_buy_members WHERE group_buy_id=$1`, g.ID) != 1 || f.balance(t, 1) != 10000 {
		t.Fatal("missing budget port changed group or money")
	}
}
