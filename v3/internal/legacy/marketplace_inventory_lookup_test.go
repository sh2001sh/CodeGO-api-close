package legacy

import "testing"

func TestMarketplaceInventoryLookupPreservesIssuanceAndFirstOrder(t *testing.T) {
	first := marketplaceFixtureRow(t, `{"id":7,"trade_no":" paid "}`)
	duplicate := marketplaceFixtureRow(t, `{"id":8,"trade_no":"paid"}`)
	missing := marketplaceFixtureRow(t, `{"id":9,"trade_no":"unissued"}`)
	purchase := marketplaceSourceRow{"request_id": []byte(`"` + marketplaceCashRequest("paid") + `"`)}
	d := &marketplaceData{source: map[string][]marketplaceSourceRow{
		"blind_box_orders":            {first, duplicate, missing},
		"balance_blind_box_purchases": {purchase},
	}}
	for i := 0; i < 2; i++ {
		if !d.hasOrderInventory(first) || !d.hasOrderInventory(duplicate) || d.hasOrderInventory(missing) {
			t.Fatal("paid inventory must not be issued again; missing inventory must remain unissued")
		}
		if got := d.externalOrderForPurchase(marketplaceCashRequest("paid")); got != 7 {
			t.Fatalf("duplicate trade must retain the first source order, got %d", got)
		}
		if got := d.externalOrderForPurchase("unknown"); got != 0 {
			t.Fatalf("unknown request must not acquire an external order, got %d", got)
		}
	}
}
