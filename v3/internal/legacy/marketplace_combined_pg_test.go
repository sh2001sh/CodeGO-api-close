//go:build pgintegration

package legacy

import (
	"context"
	"testing"
)

func TestMarketplaceCombinedReadOnlyImportReplayAndCheck(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedCommerceFixture(t, source)
	seedMarketplaceFixture(t, source)
	seedFundingFixture(t, source)
	ctx := context.Background()
	reader := readonlySource(t, source)
	importer := NewImporter(reader, target, crypto)
	preview, err := importer.Import(ctx, false)
	if err != nil || preview.Applied || len(preview.Issues) != 0 || preview.Counts["marketplace.blind_box_props"] != 2 {
		t.Fatalf("composed preview=%+v err=%v", preview, err)
	}
	for i := 0; i < 2; i++ {
		r, err := importer.Import(ctx, true)
		if err != nil || !r.Applied || len(r.Issues) != 0 {
			t.Fatalf("composed import %d=%+v err=%v", i, r, err)
		}
	}
	check, err := importer.Check(ctx)
	if err != nil || len(check.Issues) != 0 || check.Counts["check:marketplace"] == 0 {
		t.Fatalf("composed check=%+v err=%v", check, err)
	}
	if _, err := target.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET used_discount_micro=used_discount_micro+1 WHERE id=44`); err != nil {
		t.Fatal(err)
	}
	check, err = importer.Check(ctx)
	if err == nil || len(check.Issues) != 1 || check.Issues[0].Code != "target_mismatch" {
		t.Fatalf("full check missed changed marketplace money=%+v err=%v", check, err)
	}
	if r, err := importer.Import(ctx, true); err == nil || r.Applied {
		t.Fatalf("composed replay replaced changed money=%+v err=%v", r, err)
	}
}
