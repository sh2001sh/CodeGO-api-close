package legacy

import "testing"

func TestSampledExpiredSubscriptionNeverRestoresSpendableQuota(t *testing.T) {
	d := commerceTestData(t)
	d.historicalBonusRecurrenceWaived = true
	d.snapshot[9] = [2]int64{900, 0}
	p := commerceProjection{values: map[string]any{"id": int64(9), "state": "canceled"}}
	r := commerceTestRow(t, `{"id":9}`)
	if n, err := d.subscriptionOpening(r, p); err != nil || n != 0 {
		t.Fatalf("ended opening=%d %v", n, err)
	}
	d.snapshot[9] = [2]int64{900, 1}
	if _, err := d.subscriptionOpening(r, p); err == nil {
		t.Fatal("outstanding reservation accepted")
	}
}

func TestSampledRefundFIFOUsesCanonicalWalletWithoutCreatingFunds(t *testing.T) {
	d := commerceTestData(t)
	d.walletCanonical = map[int64]int64{7: 150, 8: 0}
	d.refundOrigins = []commerceRefundOrigin{{OrderID: 4, UserID: 7, Original: 100, Remaining: 100}, {OrderID: 2, UserID: 7, Original: 100, Remaining: 100}, {OrderID: 6, UserID: 8, Original: 200, Remaining: 50}}
	d.capRefundOriginsToCanonical()
	if d.refundOrigins[0].Remaining != 50 || d.refundOrigins[1].Remaining != 100 || d.refundOrigins[2].Remaining != 0 || d.walletCanonical[7] != 150 {
		t.Fatalf("FIFO mismatch: %+v", d.refundOrigins)
	}
	for _, o := range d.refundOrigins {
		if o.Remaining < 0 || o.Remaining > o.Original {
			t.Fatal("invalid paid origin")
		}
	}
}

func TestSampledImportedPlanDisablesFutureRewardsAndKeepsBaseCredits(t *testing.T) {
	d := commerceTestData(t)
	d.historicalBonusRecurrenceWaived = true
	p, err := d.projectPlan(d.plans[5])
	if err != nil {
		t.Fatal(err)
	}
	if p.values["group_buy_enabled"] != false || p.values["lucky_draw_enabled"] != false || p.values["group_buy_bonus"] != int64(0) || p.values["credits"] != int64(2000) {
		t.Fatalf("plan changed: %+v", p.values)
	}
}
