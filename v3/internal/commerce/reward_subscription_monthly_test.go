package commerce

import "testing"

func TestMonthlySubscriptionTierAndPurchaseBenefits(t *testing.T) {
	for tier, seconds := range map[string]int64{"lite": 900, "standard": 1800, "pro": 2700, "ultra": 3600, "none": 0, "": 0} {
		if got := monthlyTierSeconds(tier); got != seconds {
			t.Fatalf("tier %q=%d want %d", tier, got, seconds)
		}
	}
	for _, tc := range []struct {
		name, action                     string
		source, paid, used, remain, want int64
	}{
		{"normal", "subscribe", 0, 1000, 0, 0, 2700},
		{"renewal actual payment", "renew", 2700, 400, 40, 60, 1080},
		{"upgrade tier difference and usage", "upgrade", 1800, 700, 40, 60, 1620},
		{"upgrade fresh tier difference", "upgrade", 1800, 334, 0, 100, 900},
		{"upgrade unlimited uses full tier", "upgrade", 1800, 1000, 0, 0, 2700},
		{"renewal truncates seconds", "renew", 0, 333, 0, 0, 899},
		{"renewal full-price cap", "renew", 0, 1500, 0, 0, 2700},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := monthlyPurchaseSeconds(2700, tc.source, 1000, tc.paid, tc.action, tc.used, tc.remain)
			if err != nil || got != tc.want {
				t.Fatalf("duration=%d err=%v want=%d", got, err, tc.want)
			}
		})
	}
	if _, err := monthlyPurchaseSeconds(2700, 0, 0, 100, "renew", 0, 0); err == nil {
		t.Fatal("zero full price admitted")
	}
	if _, err := monthlyPurchaseSeconds(2700, 0, 100, 100, "fuel", 0, 0); err == nil {
		t.Fatal("unknown benefit action admitted")
	}
}
