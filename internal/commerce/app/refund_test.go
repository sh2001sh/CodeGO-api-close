package app

import "testing"

func TestRefundMoneyDeductsTwoPercentFee(t *testing.T) {
	gross, fee, net := refundMoney(169, 90, 100)
	if gross != 152.1 || fee != 3.04 || net != 149.06 {
		t.Fatalf("unexpected refund calculation: gross=%v fee=%v net=%v", gross, fee, net)
	}
}

func TestRefundMoneyReturnsZeroWhenQuotaIsConsumed(t *testing.T) {
	gross, fee, net := refundMoney(169, 0, 100)
	if gross != 0 || fee != 0 || net != 0 {
		t.Fatalf("expected no refund, got gross=%v fee=%v net=%v", gross, fee, net)
	}
}
