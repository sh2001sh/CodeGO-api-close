//go:build pgintegration

package catalog

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestPublishedBucketsFreezeVersionAndActualPaidRevenue(t *testing.T) {
	pool := testPool(t)
	mustExec(t, pool, `INSERT INTO v3_identity.users(id,username) VALUES(1,'policy-user');
	 INSERT INTO v3_commerce.plans(id,name,price_minor,currency,credits,period_seconds) VALUES(1,'old',100,'usd',1000,3600);
	 INSERT INTO v3_billing.accounts(id,owner_type,owner_id,kind) OVERRIDING SYSTEM VALUE VALUES(11,'subscription',21,'subscription'),(12,'subscription',22,'subscription');
	 INSERT INTO v3_commerce.subscriptions(id,user_id,plan_id,account_id,starts_at,expires_at,total_credits,renewable_credits,policy_version,recognized_revenue_credits)
	 VALUES(21,1,1,11,now()-interval '1 hour',now()+interval '1 hour',1030,1030,'standard_v2',1000),
	 (22,1,1,12,now()-interval '1 hour',now()+interval '1 hour',1000,1000,'legacy',NULL)`)
	var profiles map[int64]AccountProfile
	err := pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		var err error
		profiles, err = loadAccountProfiles(context.Background(), tx)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles[1].Subscriptions) != 2 {
		t.Fatalf("buckets=%+v", profiles[1])
	}
	for _, bucket := range profiles[1].Subscriptions {
		if bucket.AccountID == 11 {
			if bucket.PolicyVersion != "standard_v2" || bucket.RevenueMultiplierPPM == nil || *bucket.RevenueMultiplierPPM != 970873 {
				t.Fatalf("new=%+v", bucket)
			}
		}
		if bucket.AccountID == 12 {
			if bucket.PolicyVersion != "legacy" || bucket.RevenueMultiplierPPM != nil {
				t.Fatalf("unknown old=%+v", bucket)
			}
		}
	}
}
