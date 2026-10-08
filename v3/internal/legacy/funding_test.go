package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestFundingBatchesBoundedAndPropagateFailures(t *testing.T) {
	d := fundingUnitFixture(t)
	base := d.rows["funding_lots"][0]
	d.rows["funding_lots"] = nil
	for i := 0; i < exactBulkRows*2+7; i++ {
		row := commerceRow{}
		for key, raw := range base {
			row[key] = raw
		}
		row["lot_id"] = json.RawMessage(fmt.Sprintf(`"lot-%d"`, i))
		row["idempotency_key"] = json.RawMessage(fmt.Sprintf(`"credit-%d"`, i))
		d.rows["funding_lots"] = append(d.rows["funding_lots"], row)
	}
	accounts := map[fundingAccountKey]int64{{"user", "wallet", 7}: 901}
	var sizes []int
	total := 0
	err := d.batches(context.Background(), "funding_lots", accounts, func(key string, rows []map[string]any) error {
		if key != "lot_id" || len(rows) > exactBulkRows {
			t.Fatalf("invalid batch key=%s size=%d", key, len(rows))
		}
		for _, row := range rows {
			if row["account_id"] != int64(901) || row["original_amount"] != int64(200) {
				t.Fatalf("batch account/amount changed: %+v", row)
			}
		}
		sizes = append(sizes, len(rows))
		total += len(rows)
		return nil
	})
	if err != nil || total != exactBulkRows*2+7 || fmt.Sprint(sizes) != fmt.Sprint([]int{exactBulkRows, exactBulkRows, 7}) {
		t.Fatalf("sizes=%v total=%d error=%v", sizes, total, err)
	}
	want := errors.New("batch rejected")
	calls := 0
	err = d.batches(context.Background(), "funding_lots", accounts, func(string, []map[string]any) error {
		calls++
		return want
	})
	if !errors.Is(err, want) || calls != 1 {
		t.Fatalf("batch failure swallowed or continued: %v calls=%d", err, calls)
	}
}

func TestFundingMissingNativeAccountRules(t *testing.T) {
	d := fundingUnitFixture(t)
	for _, tc := range []struct {
		name string
		row  commerceRow
		fail bool
	}{
		{"funding_lots", d.rows["funding_lots"][0], true},
		{"funding_allocations", d.rows["funding_allocations"][0], false},
		{"wallet_reward_holds", d.rows["wallet_reward_holds"][0], true},
	} {
		p, err := d.projectFunding(tc.name, tc.row)
		if err != nil {
			t.Fatal(err)
		}
		err = resolveFundingAccount(nil, &p)
		if (err != nil) != tc.fail || p.values["account_id"] != nil {
			t.Fatalf("table=%s missing account behavior changed: %+v %v", tc.name, p.values, err)
		}
		switch tc.name {
		case "funding_lots":
			p.remaining = 0
			if err := resolveFundingAccount(nil, &p); err != nil {
				t.Fatalf("empty historical lot must allow missing native account: %v", err)
			}
		case "wallet_reward_holds":
			p.remaining = 0
			if err := resolveFundingAccount(nil, &p); err == nil {
				t.Fatal("fully consumed reward hold must still require its native wallet")
			}
		}
	}
}

func TestFundingBatchBytesFlushOversizedRowIndividually(t *testing.T) {
	d := fundingUnitFixture(t)
	base := d.rows["funding_lots"][0]
	d.rows["funding_lots"] = nil
	for i, length := range []int{1, exactBulkBytes + 1, 1} {
		row := commerceRow{}
		for key, raw := range base {
			row[key] = raw
		}
		row["lot_id"] = json.RawMessage(fmt.Sprintf(`"bytes-lot-%d"`, i))
		encoded, err := json.Marshal(strings.Repeat("x", length))
		if err != nil {
			t.Fatal(err)
		}
		row["reference_id"] = encoded
		d.rows["funding_lots"] = append(d.rows["funding_lots"], row)
	}
	var sizes []int
	err := d.batches(context.Background(), "funding_lots", map[fundingAccountKey]int64{{"user", "wallet", 7}: 901}, func(_ string, rows []map[string]any) error {
		sizes = append(sizes, len(rows))
		return nil
	})
	if err != nil || fmt.Sprint(sizes) != "[1 1 1]" {
		t.Fatalf("oversized row retained unrelated projections: %v %v", sizes, err)
	}
}

func fundingTestRow(t *testing.T, value string) commerceRow {
	t.Helper()
	row := commerceRow{}
	if err := json.Unmarshal([]byte(value), &row); err != nil {
		t.Fatal(err)
	}
	return row
}

func fundingUnitFixture(t *testing.T) *fundingData {
	t.Helper()
	return &fundingData{
		rows: map[string][]commerceRow{
			"funding_lots":        {fundingTestRow(t, `{"lot_id":"lot","account_id":"wallet","source":"blind_box","idempotency_key":"lot-credit","original_amount":100,"remaining_amount":60,"revenue_multiplier":0.650000,"created_at":"2026-09-29T09:00:00Z"}`)},
			"funding_allocations": {fundingTestRow(t, `{"allocation_id":"allocation","request_id":"request","lot_id":"lot","account_id":"wallet","source":"blind_box","amount":40,"revenue_multiplier":0.65,"created_at":"2026-09-29T10:00:00Z"}`)},
			"wallet_reward_holds": {fundingTestRow(t, `{"hold_id":"hold","account_id":"wallet","user_id":7,"original_amount":100,"consumed_amount":40,"idempotency_key":"hold-credit","created_at":"2026-09-29T09:00:00Z"}`)},
		},
		accounts: map[string]historyAccount{"wallet": {ID: "wallet", OwnerType: "user", OwnerID: 7, Kind: "claude_wallet", Unit: "quota"}},
		users:    map[int64]sourceUser{7: {ID: 7, CreatedAt: 1790672400}},
	}
}

func TestFundingRetainsOriginsAndUserAgeWithoutAddingBalance(t *testing.T) {
	d := fundingUnitFixture(t)
	report := Report{}
	d.validate(&report)
	if len(report.Issues) != 0 {
		t.Fatalf("valid funding fixture: %+v", report.Issues)
	}
	if report.Amounts["billing_funding_lots_remaining_amount_micro_credits"] != "120" || report.Amounts["billing_wallet_reward_holds_consumed_amount_micro_credits"] != "80" {
		t.Fatalf("exact opening provenance sums: %+v", report.Amounts)
	}
	p, err := d.projectFunding("wallet_reward_holds", d.rows["wallet_reward_holds"][0])
	if err != nil {
		t.Fatal(err)
	}
	if p.values["user_created_at"] != time.Unix(1790672400, 0).UTC() || p.values["source_account_id"] != "wallet" {
		t.Fatalf("hold provenance/age: %+v", p.values)
	}
	user := d.users[7]
	user.CreatedAt = 0
	d.users[7] = user
	p, err = d.projectFunding("wallet_reward_holds", d.rows["wallet_reward_holds"][0])
	if err != nil || p.values["user_created_at"] != nil {
		t.Fatalf("unknown source age must remain fully released: %+v %v", p.values, err)
	}
}

func TestFundingRejectsBrokenOriginsAndOverflow(t *testing.T) {
	cases := []struct {
		name string
		edit func(*fundingData)
	}{
		{"missing_lot", func(d *fundingData) { d.rows["funding_lots"] = nil }},
		{"missing_account", func(d *fundingData) { delete(d.accounts, "wallet") }},
		{"different_source", func(d *fundingData) { d.rows["funding_allocations"][0]["source"] = json.RawMessage(`"other"`) }},
		{"different_rate", func(d *fundingData) { d.rows["funding_allocations"][0]["revenue_multiplier"] = json.RawMessage(`0.66`) }},
		{"excess_allocation", func(d *fundingData) { d.rows["funding_allocations"][0]["amount"] = json.RawMessage(`41`) }},
		{"hold_owner", func(d *fundingData) { d.rows["wallet_reward_holds"][0]["user_id"] = json.RawMessage(`8`) }},
		{"overflow", func(d *fundingData) {
			d.rows["funding_lots"][0]["original_amount"] = json.RawMessage(`4611686018427387904`)
		}},
		{"unmapped_active_account", func(d *fundingData) { a := d.accounts["wallet"]; a.Kind = "unknown_wallet"; d.accounts["wallet"] = a }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := fundingUnitFixture(t)
			tc.edit(d)
			r := Report{}
			d.validate(&r)
			if len(r.Issues) == 0 {
				t.Fatal("invalid funding data was accepted")
			}
		})
	}
}

func TestFundingExcludesRetiredPointsBeforeAnyMonetaryValidation(t *testing.T) {
	for _, kind := range []string{"gpt_wallet", "points", "point_wallet", "bonus_quota", "blind_box_credits"} {
		t.Run(kind, func(t *testing.T) {
			d := fundingUnitFixture(t)
			d.accounts["retired"] = historyAccount{ID: "retired", OwnerType: "user", OwnerID: 7, Kind: kind, Unit: "points"}
			d.rows["funding_lots"] = append(d.rows["funding_lots"], fundingTestRow(t, `{"lot_id":"retired-lot","account_id":"retired","source":"retired-unknown","original_amount":9223372036854775807,"remaining_amount":9223372036854775807,"revenue_multiplier":-99}`))
			d.rows["funding_allocations"] = append(d.rows["funding_allocations"], fundingTestRow(t, `{"allocation_id":"retired-allocation","account_id":"retired","lot_id":"unknown-retired-lot","amount":9223372036854775807}`))
			report := Report{}
			d.validate(&report)
			if len(report.Issues) != 0 || report.Counts["billing_funding_lots"] != 1 || report.Counts["billing_funding_allocations"] != 1 || report.Counts["retired_features.billing_funding_lots"] != 1 || report.Counts["retired_features.billing_funding_allocations"] != 1 {
				t.Fatalf("retired points blocked current monetary data: %+v", report)
			}
			if report.Amounts["retired_features.billing_funding_lots.remaining_amount_v2_units"] != "9223372036854775807" || report.Amounts["retired_features.billing_funding_allocations.amount_v2_units"] != "9223372036854775807" || report.Amounts["billing_funding_lots_original_amount_micro_credits"] != "200" {
				t.Fatalf("retired points were converted into money: %+v", report.Amounts)
			}
		})
	}
}

func TestFundingPPMConversionUsesExactDecimalArithmetic(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int64
	}{
		{"0.65", 650000}, {"0.0000005", 1}, {"1.0000004999999999999", 1000000}, {"1e-6", 1}, {"9223372036854.775807", math.MaxInt64},
	} {
		row := fundingTestRow(t, `{"rate":`+tc.value+`}`)
		got, err := fundingPPM(row, "rate")
		if err != nil || got != tc.want {
			t.Fatalf("rate=%s got=%d want=%d err=%v", tc.value, got, tc.want, err)
		}
	}
	for _, value := range []string{`-0.1`, `"NaN"`, `"Infinity"`, `"1/3"`, `9223372036854.775808`} {
		if _, err := fundingPPM(fundingTestRow(t, `{"rate":`+value+`}`), "rate"); err == nil {
			t.Fatalf("invalid PPM rate accepted: %s", value)
		}
	}
}

func TestFundingAggregateSumsCanExceedSignedBigintWithoutRounding(t *testing.T) {
	d := fundingUnitFixture(t)
	d.rows["funding_allocations"], d.rows["wallet_reward_holds"] = nil, nil
	d.rows["funding_lots"] = []commerceRow{
		fundingTestRow(t, `{"lot_id":"large1","account_id":"wallet","source":"other","idempotency_key":"large-credit1","original_amount":4611686018427387903,"remaining_amount":4611686018427387903,"revenue_multiplier":0,"created_at":"2026-09-29T09:00:00Z"}`),
		fundingTestRow(t, `{"lot_id":"large2","account_id":"wallet","source":"other","idempotency_key":"large-credit2","original_amount":4611686018427387903,"remaining_amount":4611686018427387903,"revenue_multiplier":0,"created_at":"2026-09-29T09:00:00Z"}`),
	}
	report := Report{}
	d.validate(&report)
	if len(report.Issues) != 0 || report.Amounts["billing_funding_lots_remaining_amount_micro_credits"] != "18446744073709551612" {
		t.Fatalf("funding sums overflowed: %+v", report)
	}
}
