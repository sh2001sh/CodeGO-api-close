//go:build pgintegration

package catalog

import (
	"context"
	"testing"
	"time"
)

func TestCompileSubscriptionProvenancePoliciesAndPendingExclusion(t *testing.T) {
	pool := testPool(t)
	dec, enc := testDecrypter(t)
	seedCatalog(t, pool, enc)
	defaults, err := Compile(context.Background(), pool, dec)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{"default", "vip", "svip"} {
		if policy := defaults.SubscriptionPolicies[group]; !policy.Enabled || policy.MultiplierPPM != 1000000 {
			t.Fatalf("absent option lost actual v2 default for %s: %+v", group, policy)
		}
	}
	if _, ok := defaults.SubscriptionPolicies["unknown"]; ok {
		t.Fatal("unknown subscription group policy was fabricated")
	}
	mustExec(t, pool, `INSERT INTO v3_platform.settings(key,value) VALUES('SubscriptionGroupPolicy','{}')`)
	explicit, err := Compile(context.Background(), pool, dec)
	if err != nil || len(explicit.SubscriptionPolicies) != 0 {
		t.Fatalf("explicit empty source policy restored disabled defaults: %v", err)
	}
	mustExec(t, pool, `DELETE FROM v3_platform.settings WHERE key='SubscriptionGroupPolicy'`)
	now := time.Now().UTC().Truncate(time.Second)
	mustExec(t, pool, `INSERT INTO v3_identity.users(id,username) VALUES(1,'subscription-consumer')`)
	mustExec(t, pool, `INSERT INTO v3_commerce.plans(id,name,price_minor,credits,period_seconds,model_limits) VALUES(1,'current-package',100,1000,3600,'{"gpt-4":100}')`)
	for _, id := range []int64{1, 2} {
		mustExec(t, pool, `INSERT INTO v3_commerce.orders(id,user_id,plan_id,amount_minor,credits,period_seconds,currency,kind,provider,trade_no,state,expires_at)
		 VALUES($1,1,1,100,1000,3600,'usd','subscription','fixture','source-'||($1::bigint)::text,CASE WHEN $1=1 THEN 'paid' ELSE 'created' END,$2)`, id, now.Add(time.Hour))
	}
	for id := int64(1); id <= 6; id++ {
		mustExec(t, pool, `INSERT INTO v3_billing.accounts(id,owner_type,owner_id,kind) OVERRIDING SYSTEM VALUE VALUES($1,'subscription',$2,'subscription')`, 100+id, id)
		mustExec(t, pool, `INSERT INTO v3_commerce.subscriptions(id,user_id,plan_id,account_id,starts_at,expires_at,model_limits,model_usage) VALUES($1,1,1,$2,$3,$4,'{"gpt-4":55}','{"gpt-4":20}')`, id, 100+id, now, now.Add(time.Hour))
	}
	mustExec(t, pool, `UPDATE v3_commerce.subscriptions SET order_id=1,next_reset_at=$1 WHERE id=1`, now.Add(15*time.Minute))
	mustExec(t, pool, `UPDATE v3_commerce.subscriptions SET source='blind_box',reward_operation='fixture-reward' WHERE id=2`)
	mustExec(t, pool, `UPDATE v3_commerce.subscriptions SET source='order' WHERE id=3`)
	mustExec(t, pool, `INSERT INTO v3_commerce.subscription_operations(operation_id,subscription_id,actor_id,kind,state) VALUES('pending-conversion',4,1,'conversion','pending')`)
	mustExec(t, pool, `INSERT INTO v3_commerce.package_checkouts(order_id,user_id,request_id,target_subscription_id,source_account_id,action,state,success_url,cancel_url)
	 VALUES(2,1,'pending-checkout',5,105,'auto','preparing','https://example.test/success','https://example.test/cancel')`)
	mustExec(t, pool, `UPDATE v3_commerce.subscriptions SET deleted_at=$1 WHERE id=6`, now)
	mustExec(t, pool, `INSERT INTO v3_platform.settings(key,value) VALUES('SubscriptionGroupPolicy','{"default":{"enabled":true,"multiplier":0.2500005}}')`)
	snap, err := Compile(context.Background(), pool, dec)
	if err != nil {
		t.Fatal(err)
	}
	if policy := snap.SubscriptionPolicies["default"]; !policy.Enabled || policy.MultiplierPPM != 250001 {
		t.Fatal("subscription group policy was lost or rounded through float")
	}
	buckets := snap.AccountProfiles[1].Subscriptions
	if len(buckets) != 3 {
		t.Fatalf("pending/deleted funds admitted or current funds lost: %d", len(buckets))
	}
	for _, bucket := range buckets {
		if bucket.SubscriptionID > 3 || bucket.Models != nil || bucket.ModelLimits["gpt-4"] != 55 || bucket.ModelUsage["gpt-4"] != 20 {
			t.Fatalf("source metadata changed: %+v", bucket)
		}
		if bucket.Paid != (bucket.SubscriptionID == 1 || bucket.SubscriptionID == 3) {
			t.Fatalf("paid provenance fabricated/lost for sub=%d", bucket.SubscriptionID)
		}
		if bucket.SubscriptionID == 1 && !bucket.ExpiresAt.Equal(now.Add(15*time.Minute)) {
			t.Fatal("old period funding remained active past next reset")
		}
	}
	if accounts := snap.AccountProfiles[1].SubscriptionAccounts(now.Add(15 * time.Minute)); len(accounts) != 2 {
		t.Fatal("period bucket did not expire locally without a new publication")
	}
}
