//go:build pgintegration

package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func verifyFundingEconomicsAssembly(t *testing.T, pool *pgxpool.Pool, call apiCall, admin string, adminID int64, apiKey string) {
	t.Helper()
	ctx := context.Background()
	path := "/api/billing/funding-economics?day=2026-10-01"
	call("GET", path, "", "", 401)
	call("GET", path, admin, "", 403)
	w := call("POST", "/api/user/register", "", `{"username":"financial_user","password":"strong-password","accepted_terms_version":"2026-10-07","accepted_privacy_version":"2026-10-07","agreement_locale":"en"}`, 200)
	var registered struct {
		Data identity.Session `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &registered); err != nil || registered.Data.AccessToken == "" {
		t.Fatalf("financial user registration: %v", err)
	}
	call("GET", path, registered.Data.AccessToken, "", 403)
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET role='root' WHERE id=$1`, adminID); err != nil {
		t.Fatal(err)
	}
	// A working key owned by a root user remains outside session authorization.
	call("GET", "/api/log/token", apiKey, "", 200)
	call("GET", path, apiKey, "", 401)
	for _, query := range []string{"day=", "day=%GG", "day=bad", "day=2026-9-30", "day=2026-09-31", "day=2025-02-29", "day=2026-10-01T00:00:00Z", "day=2026-10-01&day=2026-09-30"} {
		call("GET", "/api/billing/funding-economics?"+query, admin, "", 400)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.request_economics
	(channel_id,route_pool_id,actual_amount,subscription_id,procurement_cost_multiplier_ppm,revenue_multiplier_ppm,settled_at,request_id,billing_source) VALUES
	(77,88,200,0,250000,0,'2026-09-30 16:00:00+00','control-financial-wallet','wallet'),
	(77,88,100,99,1500000,400000,'2026-10-01 00:00:00+00','control-financial-package','subscription'),
	(77,88,10,0,5000000,0,'2026-10-01 01:00:00+00','control-financial-legacy','wallet'),
	(77,88,20,0,2000000,0,'2026-10-01 02:00:00+00','control-financial-other','wallet'),
	(77,88,9007199254740993,0,1000000,0,'2026-10-01 03:00:00+00','control-financial-large','wallet'),
	(77,88,999,99,1000000,1000000,'2026-09-30 15:59:59.999999+00','control-financial-before','subscription'),
	(77,88,999,99,1000000,1000000,'2026-10-01 16:00:00+00','control-financial-after','subscription');
	INSERT INTO v3_billing.funding_lots(lot_id,source_account_id,source,idempotency_key,original_amount,remaining_amount,revenue_multiplier_ppm) VALUES
	('control-financial-topup','private-account','topup','control-financial-topup',100,0,750000),
	('control-financial-box','private-account','blind_box','control-financial-box',100,0,500000),
	('control-financial-other','private-account','other','control-financial-other',20,0,900000),
	('control-financial-large','private-account','topup','control-financial-large',9007199254740993,0,1000000);
	INSERT INTO v3_billing.funding_allocations(allocation_id,request_id,lot_id,source_account_id,source,amount,revenue_multiplier_ppm) VALUES
	('control-financial-topup','control-financial-wallet','control-financial-topup','private-account','topup',100,750000),
	('control-financial-box','control-financial-wallet','control-financial-box','private-account','blind_box',100,500000),
	('control-financial-other','control-financial-other','control-financial-other','private-account','other',20,900000),
	('control-financial-large','control-financial-large','control-financial-large','private-account','topup',9007199254740993,1000000)`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var state string
		if err := pool.QueryRow(ctx, `SELECT json_build_array((SELECT coalesce(sum(balance),0)::text FROM v3_billing.accounts),(SELECT coalesce(sum(version),0)::text FROM v3_billing.accounts),(SELECT count(*) FROM v3_billing.ledger_entries),(SELECT count(*) FROM v3_billing.balance_outbox))::text`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	before := snapshot()
	w = call("GET", path, admin, "", 200)
	var report struct {
		Data ledger.FundingDailyEconomics `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	r := report.Data
	if r.Date != "2026-10-01" || r.RecognizedRevenue != 9007199254741158 || r.RecognizedCost != 9007199254741193 || r.RecognizedProfit != -35 || r.UnattributedCost != 90 || len(r.Sources) != 5 {
		t.Fatalf("actual frozen source report=%+v", r)
	}
	expected := map[string][4]credits.Micro{
		"topup":               {9007199254741093, 9007199254741068, 9007199254741018, 50},
		"blind_box":           {100, 50, 25, 25},
		"subscription":        {100, 40, 150, -110},
		"legacy_unattributed": {10, 0, 50, -50},
		"other":               {20, 18, 40, -22},
	}
	for _, source := range r.Sources {
		got := [4]credits.Micro{source.Amount, source.Revenue, source.Cost, source.Profit}
		if want, exists := expected[source.Source]; !exists || got != want {
			t.Fatalf("actual source=%s values=%v want=%v", source.Source, got, want)
		}
	}
	for _, private := range []string{"private-account", "control-financial-", "channel_id", "route_pool_id", "user_id", "subscription_id", "request_id"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatalf("root aggregate included private detail %q", private)
		}
	}
	w = call("GET", "/api/billing/funding-economics?day=2026-09-30", admin, "", 200)
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil || report.Data.RecognizedRevenue != 999 || report.Data.RecognizedCost != 999 {
		t.Fatalf("Shanghai previous-day boundary: %v %+v", err, report.Data)
	}
	w = call("GET", "/api/billing/funding-economics?day=2026-10-02", admin, "", 200)
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil || report.Data.RecognizedRevenue != 999 || report.Data.RecognizedCost != 999 {
		t.Fatalf("Shanghai next-day boundary: %v %+v", err, report.Data)
	}
	w = call("GET", "/api/billing/funding-economics?day=2024-02-29", admin, "", 200)
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil || len(report.Data.Sources) != 0 || report.Data.RecognizedRevenue != 0 {
		t.Fatalf("empty report must remain exact zero: %v %+v", err, report.Data)
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().In(location).Format("2006-01-02")
	w = call("GET", "/api/billing/funding-economics", admin, "", 200)
	finish := time.Now().In(location).Format("2006-01-02")
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil || (report.Data.Date != start && report.Data.Date != finish) {
		t.Fatalf("default report day is not Shanghai today: %v", err)
	}
	if after := snapshot(); after != before {
		t.Fatal("reading financial reports changed live balance/version/ledger/outbox")
	}
	// Corrupt retained attribution must fail explicitly rather than fabricate totals.
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.request_economics(channel_id,route_pool_id,actual_amount,subscription_id,procurement_cost_multiplier_ppm,revenue_multiplier_ppm,settled_at,request_id,billing_source)
	 VALUES(77,88,1,0,1000000,0,'2026-10-03 00:00:00+00','control-financial-invalid','wallet');
	 INSERT INTO v3_billing.funding_allocations(allocation_id,request_id,lot_id,source_account_id,source,amount,revenue_multiplier_ppm)
	 VALUES('control-financial-invalid','control-financial-invalid','control-financial-topup','private-account','topup',2,750000)`); err != nil {
		t.Fatal(err)
	}
	w = call("GET", "/api/billing/funding-economics?day=2026-10-03", admin, "", 503)
	if strings.Contains(w.Body.String(), "allocation") || strings.Contains(w.Body.String(), "control-financial") || strings.Contains(w.Body.String(), `"data"`) {
		t.Fatal("failed financial query exposed private facts or fabricated a report")
	}
	if after := snapshot(); after != before {
		t.Fatal("failed financial report changed current money")
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET role='admin' WHERE id=$1`, adminID); err != nil {
		t.Fatal(err)
	}
	call("GET", path, admin, "", 403)
}
