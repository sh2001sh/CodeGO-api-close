//go:build pgintegration

package identity

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
)

type failedBudgetWriter struct{}

func (failedBudgetWriter) PostTx(context.Context, pgx.Tx, billing.Entry) (billing.PostResult, error) {
	return billing.PostResult{}, errors.New("posting unavailable")
}

func TestKeyBudgetLedgerAndPolicyPropagation(t *testing.T) {
	pool, _ := testDeps(t)
	c, err := NewControl(pool, ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32), BudgetPoster: ledger.NewPoster(pool)}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Register(ctx, RegisterInput{Username: "policy_user", Password: "strong-password"})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, `INSERT INTO v3_catalog.groups(name) VALUES ('vip')`)
	group, budget := "vip", int64(500001)
	k, raw, err := c.CreateKey(ctx, u.ID, KeyInput{Name: "finite", Group: &group, BudgetLimited: true, BudgetMicroCredits: &budget,
		CrossGroupRetry: true, MaxMarketplaceMultiplierPPM: 1234567, AllowedModels: []string{"model-x"}, AllowedCIDRs: []string{"10.0.0.0/8"}})
	if err != nil {
		t.Fatal(err)
	}
	if k.BudgetAccountID <= 0 {
		t.Fatal("missing budget account")
	}
	p, err := (pgLoader{pool: pool}).load(ctx, HashKey(raw))
	if err != nil {
		t.Fatal(err)
	}
	principal := p.Principal()
	if !principal.BudgetLimited || principal.BudgetAccountID != k.BudgetAccountID || !principal.CrossGroupRetry ||
		principal.MaxMarketplaceMultiplierPPM != 1234567 || !slices.Contains(principal.AllowedGroups, "vip") ||
		!slices.Equal(principal.AllowedModels, []string{"model-x"}) || len(principal.AllowedCIDRs) != 1 {
		t.Fatalf("policy lost: %+v", principal)
	}
	keys, err := c.ListKeys(ctx, u.ID, 0, 100)
	if err != nil || len(keys) != 1 || keys[0].BudgetMicroCredits == nil || *keys[0].BudgetMicroCredits != budget {
		t.Fatalf("finite key list=%+v err=%v", keys, err)
	}
	in := keys[0].KeyInput
	budget = 123
	in.BudgetMicroCredits = &budget
	if err = c.UpdateKey(ctx, u.ID, in); err != nil {
		t.Fatal(err)
	}
	var balance, entries, outbox int64
	err = pool.QueryRow(ctx, `SELECT balance,(SELECT count(*) FROM v3_billing.ledger_entries WHERE account_id=a.id),
	 (SELECT count(*) FROM v3_billing.balance_outbox WHERE account_id=a.id) FROM v3_billing.accounts a WHERE id=$1`, k.BudgetAccountID).Scan(&balance, &entries, &outbox)
	if err != nil || balance != budget || entries != 2 || outbox != 2 {
		t.Fatalf("ledger=%d entries=%d outbox=%d err=%v", balance, entries, outbox, err)
	}
	if err = c.UpdateKeyStatus(ctx, u.ID, k.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	keys, err = c.ListKeys(ctx, u.ID, 0, 100)
	if err != nil || !keys[0].BudgetLimited || !keys[0].CrossGroupRetry || len(keys[0].AllowedModels) != 1 {
		t.Fatalf("status-only edit erased policy: %+v %v", keys, err)
	}
	// No ledger writer may leave behind a finite key or account after failure.
	c.cfg.BudgetPoster = nil
	if _, _, err = c.CreateKey(ctx, u.ID, KeyInput{Name: "missing-writer", BudgetLimited: true}); err == nil {
		t.Fatal("missing writer accepted")
	}
	c.cfg.BudgetPoster = failedBudgetWriter{}
	if _, _, err = c.CreateKey(ctx, u.ID, KeyInput{Name: "failed-writer", BudgetLimited: true, BudgetMicroCredits: &budget}); err == nil {
		t.Fatal("posting failure accepted")
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_identity.api_keys WHERE user_id=$1`, u.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rolled back keys=%d err=%v", count, err)
	}
}

func TestKeyGroupsCannotEscalateThroughSelfSettings(t *testing.T) {
	pool, _ := testDeps(t)
	seedKey(t, pool, 1, 10)
	seedKey(t, pool, 2, 20)
	c, _ := NewControl(pool, ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32)}, discardLogger())
	mustExec(t, pool, `INSERT INTO v3_catalog.groups(name) VALUES ('vip'),('premium'),('market-private'),('personal-one')`)
	mustExec(t, pool, `UPDATE v3_identity.users SET settings='{"allowed_groups":["premium","market-private","personal-one"],"email_verified":true}' WHERE id=1`)
	mustExec(t, pool, `INSERT INTO v3_catalog.channels(id,name,provider,scope,owner_user_id) VALUES (100,'private','openai','marketplace',2)`)
	mustExec(t, pool, `INSERT INTO v3_channelmarket.groups(id,public_channel_id,channel_id,owner_user_id,public_slug,internal_group_name,display_name,lifecycle_status)
	 VALUES ('private','private',100,2,'private','market-private','Private','active')`)
	mustExec(t, pool, `INSERT INTO v3_channelmarket.route_pools(id,owner_user_id,name,internal_group_name) VALUES ('personal',2,'Personal','personal-one')`)
	for _, group := range []string{"premium", "market-private", "personal-one"} {
		if _, _, err := c.CreateKey(ctx, 1, KeyInput{Group: &group}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("unauthorized group %s: %v", group, err)
		}
	}
	for _, alias := range []string{"zero-hour", "monthly-pass"} {
		if _, _, err := c.CreateKey(ctx, 1, KeyInput{Group: &alias}); err != nil {
			t.Fatalf("virtual alias cannot reach entitlement planner: %s %v", alias, err)
		}
	}
	group := "vip"
	if _, _, err := c.CreateKey(ctx, 1, KeyInput{Group: &group}); err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, `INSERT INTO v3_channelmarket.group_access(group_id,user_id) VALUES ('private',1)`)
	group = "market-private"
	if _, _, err := c.CreateKey(ctx, 1, KeyInput{Group: &group}); err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, `INSERT INTO v3_channelmarket.channel_user_blocks(channel_id,user_id) VALUES (100,1)`)
	if _, _, err := c.CreateKey(ctx, 1, KeyInput{Group: &group}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("blocked private group: %v", err)
	}
	group = "personal-one"
	if _, _, err := c.CreateKey(ctx, 2, KeyInput{Group: &group}); err != nil {
		t.Fatal(err)
	}
	group = "auto"
	k, _, err := c.CreateKey(ctx, 1, KeyInput{Group: &group})
	if err != nil || !k.CrossGroupRetry {
		t.Fatalf("auto retry=%+v %v", k, err)
	}
	mustExec(t, pool, `INSERT INTO v3_platform.settings(key,value) VALUES ('UserUsableGroups','{"premium":"Premium","market-private":"leak","personal-one":"leak"}'),
	 ('group_ratio_setting.group_special_usable_group','{"default":{"-:premium":"remove","+:vip":"grant"}}')`)
	var allowed []string
	if err = pool.QueryRow(ctx, `SELECT ARRAY(SELECT v3_identity.allowed_groups(1))`).Scan(&allowed); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(allowed, "vip") || slices.Contains(allowed, "premium") || slices.Contains(allowed, "market-private") || slices.Contains(allowed, "personal-one") {
		t.Fatalf("operator group filtering=%v", allowed)
	}
}
