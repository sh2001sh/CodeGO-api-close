package ledger

import (
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing"
)

func TestCardEventRejectsInconsistentMetadata(t *testing.T) {
	for name, change := range map[string]func(map[string]any){
		"wrong after": func(v map[string]any) { v[billing.FieldCardAfter] = "899" },
		"negative":    func(v map[string]any) { v[billing.FieldCardBefore] = "-1000" },
		"wrong prop":  func(v map[string]any) { v[billing.FieldCardID] = "NaN" },
		"secondary":   func(v map[string]any) { v["funding_part"] = "secondary" },
		"missing ID":  func(v map[string]any) { delete(v, billing.FieldCardID) },
		"bad channel": func(v map[string]any) { v[billing.FieldChannelID] = "0" },
		"bad total":   func(v map[string]any) { v["funding_part"], v["usage_total_amount"] = "primary", "overflow" },
	} {
		t.Run(name, func(t *testing.T) {
			values := map[string]any{billing.FieldRequestID: "card", billing.FieldAccountID: "1", billing.FieldAmount: "900",
				billing.FieldChannelID: "3", billing.FieldCardID: "81", billing.FieldCardBefore: "1000", billing.FieldCardAfter: "900"}
			change(values)
			if _, err := parseEvent("1-0", values); !errors.Is(err, errMalformed) {
				t.Fatalf("inconsistent audit was not quarantined: %v", err)
			}
		})
	}
}
