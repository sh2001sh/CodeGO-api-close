package ledger

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestProviderTopupRefundV2RequiresMatchingProviderOperation(t *testing.T) {
	for _, tc := range []struct {
		kind, operation, trade, want string
		amount                       int64
	}{
		{"adjustment", "order:refund:later:200", "later", "later", -20},
		{"adjustment", "order:refund:later", "later", "later", -20},
		{"adjustment", "order:refund:later-other:200", "later", "", -20},
		{"adjustment", "admin:correction", "later", "", -20},
		{"refund", "user-refund:later:reserve", "later", "", -20},
		{"usage", "order:refund:later:200", "later", "", -20},
		{"adjustment", "order:refund:later:200", "", "", -20},
		{"adjustment", "order:refund:later:200", "later", "", 20},
	} {
		t.Run(tc.kind+tc.operation+tc.trade, func(t *testing.T) {
			e := billing.Entry{Kind: tc.kind, OperationID: tc.operation, Metadata: map[string]any{"refund_trade_no": tc.trade}}
			e.Amount = credits.Micro(tc.amount)
			if got := fundingTopupRefundTrade(e); got != tc.want {
				t.Fatalf("provider-origin gate=%q want %q", got, tc.want)
			}
		})
	}
}

func TestOwnerTopupRefundV2RequiresMatchingDurableReservation(t *testing.T) {
	for _, operation := range []string{"user-refund:owner:reserve", "user-refund:other:reserve", "user-refund:owner:release", "admin:refund"} {
		e := billing.Entry{Amount: -20, Kind: "refund", OperationID: operation, Metadata: map[string]any{"refund_trade_no": "paid", "refund_no": "owner"}}
		want := ""
		if operation == "user-refund:owner:reserve" {
			want = "paid"
		}
		if got := fundingTopupRefundTrade(e); got != want {
			t.Fatalf("owner-origin gate for %s=%q want %q", operation, got, want)
		}
	}
}
