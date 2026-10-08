package legacy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

func TestHistoricalFullyReclaimedDefaultOnlyNormalizesExplicitZero(t *testing.T) {
	for _, tc := range []struct {
		status, reclaimed, net string
		want                   int64
		bad                    bool
	}{
		{"pending", "0", "95", 0, false}, {"released", "0", "95", 0, false},
		{"forfeited", "0", "95", 0, false}, {"released", "40", "95", 80, false},
		{"reclaimed", "0", "95", 190, false}, {"reclaimed", "95", "95", 190, false},
		{"reclaimed", "1", "95", 0, true}, {"reclaimed", "96", "95", 0, true},
		{"reclaimed", "-1", "95", 0, true}, {"reclaimed", "0.5", "95", 0, true},
		{"reclaimed", "null", "95", 0, true}, {"reclaimed", "0", "95.5", 0, true},
		{"pending", "1", "95", 0, true},
		{"reclaimed", "0", "4611686018427387903", math.MaxInt64 - 1, false},
		{"reclaimed", "0", "4611686018427387904", 0, true},
	} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			d := cmUnitData(t)
			d.prepare("")
			row := cmTestRow(t, fmt.Sprintf(`{"id":"reclaim","request_id":"req","group_id":"g-201","owner_user_id":7,"consumer_user_id":8,"consumer_amount":500,"settlement_gross_amount":%s,"platform_commission":0,"transaction_fee":0,"owner_net_amount":%s,"reclaimed_amount":%s,"multiplier":1,"subscription_multiplier":0,"status":%q}`, tc.net, tc.net, tc.reclaimed, tc.status))
			before, _ := json.Marshal(row)
			record, err := d.projectSettlement(row)
			after, _ := json.Marshal(row)
			if !bytes.Equal(before, after) {
				t.Fatal("source row mutated")
			}
			if (err != nil) != tc.bad || (!tc.bad && record.values["reclaimed_micro"] != tc.want) {
				t.Fatalf("projection=%+v err=%v", record.values, err)
			}
		})
	}
}

func TestHistoricalClaudeEconomicsAliasPreservesPreciseMoneyAndRejectsUnknown(t *testing.T) {
	for _, tc := range []struct {
		source, amount, subscription string
		wantSource                   string
		wantAmount                   int64
		bad                          bool
	}{
		{"claude_wallet", "9007199254740993", "0", "wallet", 18014398509481986, false},
		{"wallet", "0", "0", "wallet", 0, false}, {"subscription", "40", "9", "subscription", 80, false},
		{"claude_wallet", "4611686018427387903", "0", "wallet", math.MaxInt64 - 1, false},
		{"claude_wallet", "4611686018427387904", "0", "", 0, true},
		{"claude_wallet", "0.5", "0", "", 0, true}, {"claude_wallet", "-1", "0", "", 0, true},
		{"claude_wallet", "40", "9", "", 0, true}, {"unknown", "40", "0", "", 0, true},
	} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			row := fundingTestRow(t, fmt.Sprintf(`{"request_id":"economics","billing_source":%q,"actual_amount":%s,"channel_id":13,"route_pool_id":0,"subscription_id":%s,"procurement_cost_multiplier":0.123456,"revenue_multiplier":0.654321,"settled_at":"2026-08-01T00:00:00Z","created_at":"2026-08-01T00:00:00Z"}`, tc.source, tc.amount, tc.subscription))
			before, _ := json.Marshal(row)
			projection, err := (&fundingData{}).projectFunding("request_economics", row)
			after, _ := json.Marshal(row)
			if !bytes.Equal(before, after) {
				t.Fatal("source economics mutated")
			}
			if (err != nil) != tc.bad {
				t.Fatalf("projection=%+v err=%v", projection.values, err)
			}
			if !tc.bad && (projection.values["billing_source"] != tc.wantSource || projection.values["actual_amount"] != tc.wantAmount || projection.values["procurement_cost_multiplier_ppm"] != int64(123456) || projection.values["revenue_multiplier_ppm"] != int64(654321)) {
				t.Fatalf("source amount or exact multipliers changed: %+v", projection.values)
			}
		})
	}
}
