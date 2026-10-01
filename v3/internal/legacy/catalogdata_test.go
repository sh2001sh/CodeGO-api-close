package legacy

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestCatalogDataPreservesExactCostsAndHistoricalRows(t *testing.T) {
	d := &catalogData{channels: map[int64]commerceRow{13: commerceTestRow(t, `{"priority":7,"weight":0}`)}}
	r, err := d.project("route_pool_members", commerceTestRow(t, `{"id":9007199254740993,"route_pool_id":51,"channel_id":13,"cost_multiplier":0.123456789012345678,"model_cost_overrides":"{\"chat\":0.999999999999999999}","fault_domain":"upstream-a","enabled":false,"deleted_at":"2026-09-29T12:34:56Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.id != 9007199254740993 || r.fields["cost_multiplier"] != "0.123456789012345678" || string(r.fields["model_cost_overrides"].(json.RawMessage)) != `{"chat":0.999999999999999999}` || r.fields["enabled"] != false || r.fields["priority"] != int64(7) || r.fields["weight"] != int64(1) {
		t.Fatalf("cost/ID/state changed: %+v", r)
	}
	if !r.fields["deleted_at"].(time.Time).Equal(time.Date(2026, 9, 29, 12, 34, 56, 0, time.UTC)) {
		t.Fatal("soft deletion timestamp changed")
	}
	pool, err := d.project("route_pools", commerceTestRow(t, `{"id":51,"name":"pool","group":"default","enabled":true,"auto_discover":true,"multiplier_weight":35,"ttft_weight":25,"cache_weight":15,"success_weight":25}`))
	if err != nil || pool.fields["model"] != "*" || pool.fields["strategy"] != "scored" || pool.fields["auto_discover"] != true {
		t.Fatalf("pool=%+v err=%v", pool, err)
	}
}

func TestCatalogDataRejectsInvalidReferencesAndCosts(t *testing.T) {
	for _, invalid := range []string{
		`{"id":1,"route_pool_id":51,"channel_id":13,"cost_multiplier":0}`,
		`{"id":1,"route_pool_id":51,"channel_id":13,"cost_multiplier":1,"model_cost_overrides":"{\"chat\":-0.1}"}`,
		`{"id":1,"route_pool_id":51,"channel_id":13,"cost_multiplier":1,"model_cost_overrides":"{\"chat\":\"1.2\"}"}`,
		`{"id":9223372036854775808,"route_pool_id":51,"channel_id":13,"cost_multiplier":1}`,
	} {
		d := &catalogData{channels: map[int64]commerceRow{13: commerceTestRow(t, `{}`)}}
		if _, err := d.project("route_pool_members", commerceTestRow(t, invalid)); err == nil {
			t.Fatalf("accepted invalid member: %s", invalid)
		}
	}
	d := &catalogData{channels: map[int64]commerceRow{}, rows: map[string][]commerceRow{
		"models":             {commerceTestRow(t, `{"id":31,"model_name":"chat","vendor_id":999}`)},
		"route_pool_members": {commerceTestRow(t, `{"id":61,"route_pool_id":999,"channel_id":999,"cost_multiplier":1}`)},
	}}
	var report Report
	d.validate(&report)
	if len(report.Issues) != 2 || report.Issues[0].Code != "catalog_vendor_missing" || report.Issues[1].Code != "catalog_pool_reference_missing" {
		t.Fatalf("reference validation=%+v", report.Issues)
	}
	row := commerceTestRow(t, `{"id":1,"model_name":"chat","status":1}`)
	row["sync_official"], _ = json.Marshal(int64(math.MaxInt32) + 1)
	if _, err := d.project("models", row); err == nil {
		t.Fatal("accepted bigint value into integer status")
	}
}

func TestCatalogDataPrefillEndpointUsesNativeJSON(t *testing.T) {
	d := &catalogData{}
	for _, tc := range []struct{ row, want string }{
		{`{"id":1,"name":"models","type":"model","items":["chat"]}`, `["chat"]`},
		{`{"id":2,"name":"endpoint","type":"endpoint","items":"{\"chat\":{\"path\":\"/v1/chat/completions\"}}"}`, `{"chat":{"path":"/v1/chat/completions"}}`},
		{`{"id":3,"name":"tags","type":"tag","items":"alpha,beta"}`, `["alpha","beta"]`},
	} {
		r, err := d.project("prefill_groups", commerceTestRow(t, tc.row))
		if err != nil || string(r.fields["items"].(json.RawMessage)) != tc.want {
			t.Fatalf("prefill=%+v err=%v", r, err)
		}
	}
	for _, invalid := range []string{`{"id":1,"name":"unknown","type":"unknown"}`, `{"id":1,"name":"models","type":"model","items":[1]}`, `{"id":1,"name":"endpoint","type":"endpoint","items":"not-json"}`} {
		if _, err := d.project("prefill_groups", commerceTestRow(t, invalid)); err == nil {
			t.Fatalf("accepted invalid prefill: %s", invalid)
		}
	}
}
