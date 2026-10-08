//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"
)

func TestLegacyFundingCheckPreservesNativeV2DefaultsAndRejectsExtraOldPolicy(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedCommerceFixture(t, source)
	seedFundingFixture(t, source)
	ctx := context.Background()
	importer := NewImporter(readonlySource(t, source), target, crypto)
	if _, err := importer.Import(ctx, true); err != nil {
		t.Fatal(err)
	}
	if report, err := importer.Check(ctx); err != nil {
		t.Fatalf("new native source policies must not fail old reconciliation: %s %v", fundingReportDetails(report), err)
	}
	var count int
	if err := target.QueryRow(ctx, `SELECT count(*) FROM v3_billing.funding_source_policies WHERE source IN('referral_reward','subscription_conversion','blind_box_batch_base','blind_box_batch_reward') AND revenue_multiplier_ppm=0`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("native policies overwritten count=%d err=%v", count, err)
	}
	var legacy, unknown bool
	if err := target.QueryRow(ctx, `SELECT bool_and(policy_version='legacy'),bool_and(recognized_revenue_credits IS NULL) FROM v3_commerce.subscriptions`).Scan(&legacy, &unknown); err != nil || !legacy || !unknown {
		t.Fatalf("import changed old rules or invented income legacy=%v unknown=%v err=%v", legacy, unknown, err)
	}
	if _, err := target.Exec(ctx, `UPDATE v3_billing.funding_source_policies SET revenue_multiplier_ppm=1 WHERE source='blind_box_batch_base'`); err != nil {
		t.Fatal(err)
	}
	if report, err := importer.Check(ctx); err == nil || !strings.Contains(fundingReportDetails(report), "record count differs") {
		t.Fatalf("changed native valuation escaped reconciliation: %s %v", fundingReportDetails(report), err)
	}
	if _, err := target.Exec(ctx, `UPDATE v3_billing.funding_source_policies SET revenue_multiplier_ppm=0 WHERE source='blind_box_batch_base'`); err != nil {
		t.Fatal(err)
	}
	if _, err := target.Exec(ctx, `INSERT INTO v3_billing.funding_source_policies(source,revenue_multiplier_ppm) VALUES('topup',1000000)`); err != nil {
		t.Fatal(err)
	}
	if report, err := importer.Check(ctx); err == nil || !strings.Contains(fundingReportDetails(report), "record count differs") {
		t.Fatalf("unexpected old policy escaped check: %s %v", fundingReportDetails(report), err)
	}
}
