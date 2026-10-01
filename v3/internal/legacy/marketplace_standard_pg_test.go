//go:build pgintegration

package legacy

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

func TestMarketplaceImportedStandardInventoryKeepsEffectivePolicy(t *testing.T) {
	source, target, crypto := importTestDB(t)
	ctx := context.Background()
	d := marketplaceFullFixture(t)
	d.source["options"] = []marketplaceSourceRow{
		marketplaceFixtureRow(t, `{"key":"blind_box_setting.enabled","value":"true"}`),
		marketplaceFixtureRow(t, `{"key":"blind_box_setting.subscription_prize_probability","value":"0"}`),
	}
	sources := marketplaceSourceFixture(t, source, d)
	err := pgx.BeginTxFunc(ctx, source, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		var err error
		d, err = loadMarketplace(ctx, tx, sources)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = target.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(7,'alice'),(8,'bob');
	INSERT INTO v3_commerce.plans(id,name,price_minor,credits,period_seconds) VALUES(3,'Lite月卡',100,1000,86400);
	INSERT INTO v3_billing.accounts(id,owner_type,owner_id,kind) OVERRIDING SYSTEM VALUE VALUES(90,'subscription',9,'subscription');
	INSERT INTO v3_commerce.subscriptions(id,user_id,plan_id,account_id,starts_at,expires_at) VALUES(9,7,3,90,to_timestamp(1800000000),to_timestamp(1800086400));`)
	if err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(source, target, crypto)
	if err := pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error { return importer.importMarketplace(ctx, tx, d) }); err != nil {
		t.Fatal(err)
	}
	service := marketplace.New(target, ledger.NewPoster(target), ledger.NewAccounts(target), nil, nil, marketplace.Config{
		Now: func() time.Time { return time.Unix(1800000003, 0) },
		Draw: func(limit int64) (int64, error) {
			if limit == 10000000 {
				return limit - 1, nil // Miss hidden zero-hour; select first ordinary tier.
			}
			return 0, nil
		},
	})
	for i := 0; i < 2; i++ {
		records, err := service.OpenBoxes(ctx, 7, "imported-standard-open", 1)
		if err != nil || len(records) != 1 {
			t.Fatalf("standard retained inventory %d=%+v err=%v", i, records, err)
		}
		r := records[0]
		if r.ItemID >= 0 || r.Reward.Amount != 200000 || r.Reward.LegacyRewardType != "claude_quota" || r.Reward.WalletType != "claude" || r.Guarantee != "none" {
			t.Fatalf("standard stock silently became unified first reward: %+v", r)
		}
	}
	var balance, entries, opened, progress, points int64
	if err := target.QueryRow(ctx, `SELECT
	(SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'),
	(SELECT count(*) FROM v3_billing.ledger_entries),
	(SELECT opened_count FROM v3_marketplace.blind_box_orders WHERE id=1),
	(SELECT small_progress FROM v3_marketplace.blind_box_pity WHERE user_id=7 AND pool_id=2),
	(SELECT points FROM v3_marketplace.blind_box_zero_hour_states WHERE user_id=7)`).Scan(&balance, &entries, &opened, &progress, &points); err != nil {
		t.Fatal(err)
	}
	if balance != 200000 || entries != 1 || opened != 2 || progress != 5 || points != 105 {
		t.Fatalf("replay/state: balance=%d entries=%d opened=%d progress=%d points=%d", balance, entries, opened, progress, points)
	}
}
