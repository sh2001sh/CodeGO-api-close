//go:build pgintegration

package legacy

import (
	"context"
	"testing"
)

func TestFullOfflineImportAcrossNativeDomains(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedCommerceFixture(t, source)
	seedChannelMarketFixture(t, source)
	seedMarketplaceFixture(t, source)
	historyFixture(t, source)
	seedFundingFixture(t, source)
	seedCatalogDataFixture(t, source)
	seedOIDCFixture(t, source)
	ctx := context.Background()
	importer := NewImporter(readonlySource(t, source), target, crypto)
	preview, err := importer.Import(ctx, false)
	if err != nil || len(preview.Issues) != 0 || preview.Applied {
		t.Fatalf("full dry-run=%+v err=%v", preview, err)
	}
	for pass := range 2 {
		applied, importErr := importer.Import(ctx, true)
		if importErr != nil || !applied.Applied {
			t.Fatalf("full apply %d=%+v err=%v", pass, applied, importErr)
		}
		checked, checkErr := importer.Check(ctx)
		if checkErr != nil || len(checked.Issues) != 0 {
			t.Fatalf("full check %d=%+v err=%v", pass, checked, checkErr)
		}
		if checked.Counts["ledger_balance_differences"] != 0 || checked.Counts["check:oidc"] != 3 {
			t.Fatalf("full ledger/OIDC reconciliation=%+v", checked)
		}
	}
	if _, err = target.Exec(ctx, `UPDATE v3_identity.oidc_tokens SET revoked_at=NULL`); err != nil {
		t.Fatal(err)
	}
	checked, err := importer.Check(ctx)
	if err == nil || len(checked.Issues) == 0 {
		t.Fatalf("revoked grant mutation accepted: %+v err=%v", checked, err)
	}
}
