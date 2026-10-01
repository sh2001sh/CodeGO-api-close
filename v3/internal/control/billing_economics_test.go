package control

import (
	"net/url"
	"testing"
	"time"
)

func TestFundingEconomicsShanghaiDayAndStrictInput(t *testing.T) {
	before := time.Date(2026, 9, 30, 15, 59, 59, 0, time.UTC)
	for _, tc := range []struct {
		query string
		now   time.Time
		want  string
	}{
		{"", before, "2026-09-30"},
		{"", before.Add(time.Second), "2026-10-01"},
		{"day=2024-02-29", before, "2024-02-29"},
		{"day=2026-10-01", before, "2026-10-01"},
	} {
		query, err := url.ParseQuery(tc.query)
		if err != nil {
			t.Fatal(err)
		}
		day, err := fundingEconomicsDay(query, tc.now)
		if err != nil || day.Format("2006-01-02") != tc.want || day.Hour() != 0 || day.Location().String() != "Asia/Shanghai" {
			t.Fatalf("query=%q day=%v want=%s err=%v", tc.query, day, tc.want, err)
		}
	}
	for _, raw := range []string{"", "2026-9-30", "2026-09-31", "2025-02-29", "2026-10-01T00:00:00Z", " 2026-10-01", "0000-01-01"} {
		if _, err := fundingEconomicsDay(url.Values{"day": {raw}}, before); err == nil {
			t.Errorf("accepted invalid day=%q", raw)
		}
	}
	if _, err := fundingEconomicsDay(url.Values{"day": {"2026-09-30", "2026-10-01"}}, before); err == nil {
		t.Fatal("accepted ambiguous duplicate report days")
	}
}
