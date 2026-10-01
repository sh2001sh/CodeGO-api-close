//go:build pgintegration

package catalog

import (
	"context"
	"reflect"
	"testing"
)

func TestCompileActualFundingPreferenceAndSelectiveInvalidation(t *testing.T) {
	pool := testPool(t)
	dec, enc := testDecrypter(t)
	seedCatalog(t, pool, enc)
	mustExec(t, pool, `INSERT INTO v3_identity.users(id,username) VALUES(1,'preference-consumer')`)
	ctx := context.Background()
	countCatalog := func() int {
		t.Helper()
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_platform.cache_invalidation_outbox WHERE entity='catalog' AND entity_id='1'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	before := countCatalog()
	mustExec(t, pool, `UPDATE v3_identity.users SET settings='{"theme":"dark"}' WHERE id=1`)
	if countCatalog() != before {
		t.Fatal("unrelated UI settings republished catalog funding policy")
	}
	mustExec(t, pool, `UPDATE v3_identity.users SET settings=settings||'{"billing_preference":"subscription_first","funding_source_order":["wallet","subscription"],"subscription_order_ids":[9007199254740993,17]}'::jsonb WHERE id=1`)
	if countCatalog() != before+1 {
		t.Fatal("saving actual funding preference did not invalidate catalog snapshot")
	}
	snap, err := Compile(ctx, pool, dec)
	if err != nil {
		t.Fatal(err)
	}
	profile := snap.AccountProfiles[1]
	if profile.BillingPreference != "wallet_first" || !reflect.DeepEqual(profile.FundingSourceOrder, []string{"wallet", "subscription"}) || !reflect.DeepEqual(profile.SubscriptionOrderIDs, []int64{9007199254740993, 17}) {
		t.Fatalf("actual settings preference or exact subscription IDs lost: %+v", profile)
	}
	mustExec(t, pool, `UPDATE v3_identity.users SET settings=settings||'{"theme":"light"}'::jsonb WHERE id=1`)
	if countCatalog() != before+1 {
		t.Fatal("unrelated settings overwrote funding policy invalidation boundary")
	}
	mustExec(t, pool, `UPDATE v3_identity.users SET settings=settings||'{"funding_source_order":["wallet","wallet"]}'::jsonb WHERE id=1`)
	if _, err := Compile(ctx, pool, dec); err == nil {
		t.Fatal("invalid stored explicit preference silently granted fallback funding")
	}
	mustExec(t, pool, `UPDATE v3_identity.users SET settings='{}' WHERE id=1`)
	snap, err = Compile(ctx, pool, dec)
	if err != nil || snap.AccountProfiles[1].BillingPreference != "subscription_first" {
		t.Fatalf("missing preference did not restore actual default: %v", err)
	}
}
