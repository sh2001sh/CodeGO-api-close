//go:build pgintegration

package legacy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

func TestCommerceImportedRedemptionTypesRemainConsumable(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedCommerceFixture(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `INSERT INTO migration_source.redemptions VALUES
	 (4,7,'original-active-blind-box-code',1,'Active boxes','blind_box',0,'claude',0,'',2,1700000000,0,0,0,NULL)`); err != nil {
		t.Fatal(err)
	}
	reader := readonlySource(t, source)
	importer := NewImporter(reader, target, crypto)
	if report, err := importer.Import(ctx, true); err != nil || !report.Applied {
		t.Fatalf("source code import=%+v err=%v", report, err)
	}
	if report, err := importer.Check(ctx); err != nil || len(report.Issues) != 0 {
		t.Fatalf("offline code check=%+v err=%v", report, err)
	}
	now := func() time.Time { return time.Unix(1700000300, 0).UTC() }
	poster := ledger.NewPoster(target)
	service := commerce.New(target, poster, nil, commerce.Config{Now: now})
	for i := 0; i < 2; i++ {
		result, err := service.RedeemTyped(ctx, 7, "original-unused-code")
		if err != nil || result.RedeemType != "credits" || result.Credits != 246 {
			t.Fatalf("migrated quota redemption %d=%+v err=%v", i, result, err)
		}
	}
	var balance, entries int64
	if err := target.QueryRow(ctx, `SELECT balance,(SELECT count(*) FROM v3_billing.ledger_entries WHERE operation_id='redemption:1')
	 FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'`).Scan(&balance, &entries); err != nil || balance != 1246 || entries != 1 {
		t.Fatalf("migrated quota replay balance=%d entries=%d err=%v", balance, entries, err)
	}
	first, err := service.RedeemTyped(ctx, 8, "original-subscription-code")
	if err != nil || first.RedeemType != "subscription" || first.Credits != 0 || first.PlanID != 5 || first.UserSubscriptionID <= 11 {
		t.Fatalf("migrated plan redemption=%+v err=%v", first, err)
	}
	second, err := service.RedeemTyped(ctx, 8, "original-subscription-code")
	if err != nil || second.UserSubscriptionID != first.UserSubscriptionID {
		t.Fatalf("migrated plan replay=%+v err=%v", second, err)
	}
	var count int
	var modelLimits string
	err = target.QueryRow(ctx, `SELECT a.balance,s.model_limits::text,(SELECT count(*) FROM v3_commerce.subscriptions WHERE reward_operation='redemption:2')
	 FROM v3_commerce.subscriptions s JOIN v3_billing.accounts a ON a.id=s.account_id WHERE s.id=$1 AND s.user_id=8 AND s.source='redemption'`, first.UserSubscriptionID).Scan(&balance, &modelLimits, &count)
	if err != nil || balance != 800 || count != 1 || modelLimits != `{"chat-model": 500}` {
		t.Fatalf("native imported plan balance=%d count=%d limits=%s err=%v", balance, count, modelLimits, err)
	}
	if _, err = service.RedeemTyped(ctx, 7, "original-subscription-code"); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("foreign user reused imported plan code: %v", err)
	}
	if _, err = service.RedeemTyped(ctx, 7, "original-active-blind-box-code"); !errors.Is(err, commerce.ErrProviderUnavailable) {
		t.Fatalf("missing inventory port did not reject atomically: %v", err)
	}
	var state string
	if err = target.QueryRow(ctx, `SELECT state FROM v3_commerce.redemption_codes WHERE id=4`).Scan(&state); err != nil || state != "active" {
		t.Fatalf("failed blind box grant consumed migrated code: %s err=%v", state, err)
	}
	market := marketplace.New(target, poster, ledger.NewAccounts(target), service, service, marketplace.Config{Now: now})
	if _, err = market.SavePool(ctx, marketplace.Pool{Name: "Native standard inventory", Scope: "standard", Enabled: true, Price: 1000, DailyLimit: 10,
		Rewards: []marketplace.Reward{{Kind: "credits", Title: "Current monetary credits", Weight: 1, Amount: 10}}}); err != nil {
		t.Fatal(err)
	}
	service.SetCashBoxMarket(market)
	box, err := service.RedeemTyped(ctx, 7, "original-active-blind-box-code")
	if err != nil || box.RedeemType != "blind_box" || box.Credits != 0 || box.BlindBoxQuantity != 2 || box.BlindBoxOrderID <= 0 {
		t.Fatalf("migrated box redemption=%+v err=%v", box, err)
	}
	boxReplay, err := service.RedeemTyped(ctx, 7, "original-active-blind-box-code")
	if err != nil || boxReplay.BlindBoxOrderID != box.BlindBoxOrderID {
		t.Fatalf("migrated box replay=%+v err=%v", boxReplay, err)
	}
	if err = target.QueryRow(ctx, `SELECT sum(quantity) FROM v3_marketplace.blind_box_purchases WHERE user_id=7`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("migrated boxes quantity=%d err=%v", count, err)
	}
	used, err := service.RedeemTyped(ctx, 8, "original-blind-box-code")
	if err != nil || used.RedeemType != "blind_box" || used.BlindBoxQuantity != 2 || used.BlindBoxOrderID != 0 {
		t.Fatalf("historical used code reissued benefits=%+v err=%v", used, err)
	}
	if _, err = service.RedeemTyped(ctx, 7, "original-blind-box-code"); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("historical used code changed owner: %v", err)
	}
}
