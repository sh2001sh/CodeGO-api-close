package legacy

import (
	"fmt"
	"strings"
	"testing"
)

func TestCommercePlanTargetBoundsRejectDuringPreflight(t *testing.T) {
	for _, test := range []struct {
		price   string
		seconds int64
		want    string
	}{
		{"0.01", 60, ""},
		{"0.01", 31622400, ""},
		{"0", 60, "positive price"},
		{"0.004", 60, "positive price"},
		{"1", 59, "60..31622400"},
		{"1", 31622401, "60..31622400"},
	} {
		t.Run(fmt.Sprintf("price%s-seconds%d", test.price, test.seconds), func(t *testing.T) {
			d := commerceTestData(t)
			row := commerceTestRow(t, fmt.Sprintf(`{"id":5,"title":"Bounded plan","price_amount":%s,"duration_unit":"custom","custom_seconds":%d}`, test.price, test.seconds))
			d.rows["subscription_plans"] = []commerceRow{row}
			report := Report{}
			d.validate(&report)
			if test.want == "" {
				if len(report.Issues) != 0 {
					t.Fatalf("valid target boundary rejected: %+v", report.Issues)
				}
			} else if len(report.Issues) != 1 || !strings.Contains(report.Issues[0].Detail, test.want) {
				t.Fatalf("unrepresentable plan passed preflight: %+v", report.Issues)
			}
		})
	}
}
