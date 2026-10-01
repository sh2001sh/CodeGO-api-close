package legacy

import (
	"crypto/sha256"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func commerceTestRow(t *testing.T, raw string) commerceRow {
	t.Helper()
	var row commerceRow
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		t.Fatal(err)
	}
	return row
}

func commerceTestData(t *testing.T) *commerceData {
	t.Helper()
	plan := commerceTestRow(t, `{"id":5,"title":"Cycle plan","price_amount":12.123456,"currency":"USD","total_amount":1000,"period_amount":400,"duration_unit":"month","duration_value":1,"quota_reset_period":"daily","model_limits":"{\"chat\":250}","created_at":1690000000,"updated_at":1700000000}`)
	return &commerceData{rows: map[string][]commerceRow{}, plans: map[int64]commerceRow{5: plan}, users: map[int64]bool{7: true, 8: true}, options: map[string]string{}, snapshot: map[int64][2]int64{}, reservationStates: map[string][]string{}}
}

func TestCommerceExactTopupAndOrderNamespace(t *testing.T) {
	d := commerceTestData(t)
	row := commerceTestRow(t, `{"id":1,"user_id":7,"amount":9007199254740,"money":90071992547409.93,"trade_no":"legacy-topup","payment_method":"stripe","status":"success","create_time":1700000000,"complete_time":1700000100}`)
	projected, err := d.project("top_ups", row)
	if err != nil {
		t.Fatal(err)
	}
	if projected.values["credits"] != int64(9007199254740000000) || projected.values["amount_minor"] != int64(9007199254740993) || projected.values["id"] != int64(2) || projected.values["state"] != "paid" || projected.values["currency"] != "usd" {
		t.Fatalf("incorrect exact mapping: %+v", projected.values)
	}
	if !projected.values["created_at"].(time.Time).Equal(time.Unix(1700000000, 0)) {
		t.Fatal("historical timestamp changed")
	}
	row = commerceTestRow(t, `{"id":1,"user_id":7,"plan_id":5,"money":12.123456,"trade_no":"legacy-sub","payment_provider":"epay","status":"pending","create_time":1700000000}`)
	subOrder, err := d.project("subscription_orders", row)
	if err != nil {
		t.Fatal(err)
	}
	if subOrder.values["id"] != int64(3) || subOrder.values["legacy_id"] != int64(1) || subOrder.values["credits"] != int64(2000) || subOrder.values["money"] != "12.123456" {
		t.Fatalf("subscription mapping: %+v", subOrder.values)
	}
}

func TestCommerceCycleUsageAndCanonicalBalance(t *testing.T) {
	d := commerceTestData(t)
	row := commerceTestRow(t, `{"id":9,"user_id":7,"plan_id":5,"amount_total":1000,"amount_used":200,"period_amount":400,"period_used":350,"status":"active","start_time":1700000000,"end_time":1702592000,"next_reset_time":1700086400,"model_limits":"{\"chat\":250}","model_usage":"{\"chat\":100}"}`)
	projected, err := d.project("user_subscriptions", row)
	if err != nil {
		t.Fatal(err)
	}
	if projected.values["period_credits"] != int64(800) || projected.values["period_used"] != int64(700) || projected.values["model_limits"] != `{"chat":500}` || projected.values["model_usage"] != `{"chat":200}` {
		t.Fatalf("cycle lost: %+v", projected.values)
	}
	d.snapshot[9] = [2]int64{800, 0}
	amount, err := d.subscriptionOpening(row, projected)
	if err != nil || amount != 100 {
		t.Fatalf("opening=%d %v", amount, err)
	}
	d.snapshot[9] = [2]int64{800, 1}
	if _, err = d.subscriptionOpening(row, projected); err == nil {
		t.Fatal("outstanding reservation accepted")
	}
	d.snapshot[9] = [2]int64{700, 0}
	if _, err = d.subscriptionOpening(row, projected); err == nil {
		t.Fatal("inconsistent canonical balance accepted")
	}
}

func TestCommerceLegacyCycleNotResetOnImport(t *testing.T) {
	d := commerceTestData(t)
	row := commerceTestRow(t, `{"id":9,"user_id":7,"plan_id":5,"amount_total":1000,"amount_used":999,"period_amount":0,"period_used":0,"status":"active","start_time":1700000000,"end_time":1702592000}`)
	p, err := d.project("user_subscriptions", row)
	if err != nil {
		t.Fatal(err)
	}
	if p.values["legacy_periodic"] != true || p.values["period_credits"] != int64(2000) || p.values["period_used"] != int64(1998) {
		t.Fatalf("legacy cycle: %+v", p.values)
	}
	amount, err := d.subscriptionOpening(row, p)
	if err != nil || amount != 2 {
		t.Fatalf("opening=%d %v", amount, err)
	}
}

func TestCommerceRedemptionsRemainConsumable(t *testing.T) {
	d := commerceTestData(t)
	for _, kind := range []string{"quota", "subscription", "blind_box"} {
		row := commerceTestRow(t, `{"id":4,"user_id":7,"key":"legacy-code-key","status":1,"quota":123,"redeem_type":"`+kind+`","plan_id":5,"blind_box_quantity":2,"created_time":1700000000}`)
		p, err := d.project("redemptions", row)
		if err != nil {
			t.Fatal(err)
		}
		expected := sha256.Sum256([]byte("legacy-code-key"))
		expectedKind := kind
		if kind == "quota" {
			expectedKind = "credits"
		}
		if string(p.values["code_hash"].([]byte)) != string(expected[:]) || p.values["redeem_type"] != expectedKind || p.values["credits"] != int64(246) {
			t.Fatal("redemption identity or type lost")
		}
	}
	used := commerceTestRow(t, `{"id":4,"key":"legacy-code-key","status":3,"quota":123,"used_user_id":7,"redeemed_time":1700000100}`)
	p, err := d.project("redemptions", used)
	if err != nil || p.values["state"] != "used" || p.values["claimed_by"] != int64(7) {
		t.Fatalf("used code: %+v %v", p.values, err)
	}
}

func TestCommerceRefundAndInvoiceAmounts(t *testing.T) {
	d := commerceTestData(t)
	row := commerceTestRow(t, `{"id":1,"user_id":7,"amount":2,"money":2.35,"trade_no":"refunded","payment_provider":"epay","status":"success","create_time":1700000000,"complete_time":1700000100,"refund_status":"success","refund_no":"refund-1","refund_amount":1.25,"refund_quota":100,"refund_updated_at":1700000200}`)
	p, err := d.project("top_ups", row)
	if err != nil || p.values["state"] != "refunded" || p.values["refund_minor"] != int64(125) || p.values["refund_credits"] != int64(200) || p.values["refund_no"] != "refund-1" {
		t.Fatalf("refund lost: %+v %v", p.values, err)
	}
	invoice := commerceTestRow(t, `{"id":10,"user_id":7,"source_type":"topup","trade_no":"refunded","order_amount":2.35,"currency":"CNY","status":"issued","order_count":1,"invoice_type":"personal","title":"Alice","email":"alice@example.invalid","invoice_number":"INV-1","document_url":"https://invoice.invalid/1","created_at":1700000100,"issued_at":1700000200}`)
	p, err = d.project("invoice_requests", invoice)
	if err != nil || p.values["order_amount_minor"] != int64(235) || p.values["status"] != "issued" || p.values["invoice_number"] != "INV-1" || p.values["order_amount"] != "2.35" {
		t.Fatalf("invoice lost: %+v %v", p.values, err)
	}
}

func TestCommerceFailuresBlockPreview(t *testing.T) {
	for _, test := range []struct{ name, raw, want string }{
		{"top_ups", `{"id":1,"user_id":7,"amount":9223372036855,"money":2,"trade_no":"topup","status":"pending"}`, "bigint"},
		{"top_ups", `{"id":1,"user_id":7,"amount":1,"money":-2,"trade_no":"topup","status":"pending"}`, "nonnegative"},
		{"user_subscriptions", `{"id":9,"user_id":7,"plan_id":5,"amount_total":100,"amount_used":101,"status":"active","start_time":1700000000,"end_time":1702592000}`, "exceeds"},
		{"user_subscriptions", `{"id":9,"user_id":7,"plan_id":5,"amount_total":100,"amount_used":20,"model_limits":"{\"chat\":10}","model_usage":"{\"chat\":11}","status":"active","start_time":1700000000,"end_time":1702592000}`, "model usage"},
		{"subscription_pre_consume_records", `{"id":11,"user_id":7,"user_subscription_id":9,"request_id":"open","status":"consumed","pre_consumed":1}`, "settlement"},
		{"wallet_transfers", `{"id":4,"amount_quota":10,"fee_quota":1,"total_debit_quota":10,"status":"completed"}`, "total_debit"},
	} {
		t.Run(test.name+test.want, func(t *testing.T) {
			d := commerceTestData(t)
			d.rows[test.name] = []commerceRow{commerceTestRow(t, test.raw)}
			r := Report{}
			d.validate(&r)
			if len(r.Issues) == 0 || !strings.Contains(r.Issues[0].Detail, test.want) {
				t.Fatalf("report=%+v", r)
			}
		})
	}
	if _, err := commerceOrderID(math.MaxInt64, "topup"); err == nil {
		t.Fatal("order ID overflow accepted")
	}
	if _, err := commerceUnits(commerceTestRow(t, `{"x":4611686018427387904}`), "x"); err == nil {
		t.Fatal("credit overflow accepted")
	}
}

func TestCommerceConsumedRecordNeedsSettledReservation(t *testing.T) {
	d := commerceTestData(t)
	row := commerceTestRow(t, `{"id":1,"user_id":7,"user_subscription_id":9,"request_id":"done","status":"consumed","pre_consumed":10}`)
	d.reservationStates["done"] = []string{"settled", "settled"}
	if _, err := d.project("subscription_pre_consume_records", row); err != nil {
		t.Fatal(err)
	}
	d.reservationStates["done"] = []string{"settled", "open"}
	if _, err := d.project("subscription_pre_consume_records", row); err == nil {
		t.Fatal("open reservation accepted")
	}
	delete(d.reservationStates, "done")
	d.completedRequests = map[string]int64{"done": 7}
	if _, err := d.project("subscription_pre_consume_records", row); err != nil {
		t.Fatal("terminal consume log rejected", err)
	}
	d.completedRequests["done"] = 8
	if _, err := d.project("subscription_pre_consume_records", row); err == nil {
		t.Fatal("another user's consume log accepted")
	}
}

func TestCommerceCurrencyMinorBoundaries(t *testing.T) {
	row := commerceTestRow(t, `{"money":12.345}`)
	for currency, want := range map[string]int64{"USD": 1235, "JPY": 12, "KWD": 12345} {
		got, err := commerceMinor(row, "money", currency)
		if err != nil || got != want {
			t.Fatalf("%s=%d want=%d err=%v", currency, got, want, err)
		}
	}
	d := commerceTestData(t)
	for _, provider := range []string{"waffo", "waffo_pancake"} {
		p, err := d.project("top_ups", commerceTestRow(t, `{"id":1,"user_id":7,"amount":1,"money":1.25,"trade_no":"waffo-old","payment_provider":"`+provider+`","status":"pending"}`))
		if err != nil || p.values["currency"] != "usd" {
			t.Fatalf("%s default=%+v %v", provider, p.values, err)
		}
	}
}

func TestCommerceRefundOriginsCannotExceedWallet(t *testing.T) {
	d := commerceTestData(t)
	d.rows["users"] = []commerceRow{commerceTestRow(t, `{"id":7,"claude_quota":100}`)}
	d.refundOrigins = []commerceRefundOrigin{{OrderID: 2, UserID: 7, Original: 1000, Remaining: 201}}
	r := Report{}
	d.validate(&r)
	if len(r.Issues) != 1 || r.Issues[0].Code != "refund_origin_exceeds_wallet" {
		t.Fatalf("report=%+v", r)
	}
	d.refundOrigins[0].Remaining = 200
	r = Report{}
	d.validate(&r)
	if len(r.Issues) != 0 {
		t.Fatalf("boundary rejected: %+v", r)
	}
}

func TestCommerceModelNamesRetainLegacyTrimming(t *testing.T) {
	r := commerceTestRow(t, `{"model_limits":"{\" chat \":20}"}`)
	got, err := commerceModelMap(r, "model_limits")
	if err != nil || got != `{"chat":40}` {
		t.Fatalf("map=%s %v", got, err)
	}
	r = commerceTestRow(t, `{"model_limits":"{\"chat\":20,\" chat \":10}"}`)
	if _, err = commerceModelMap(r, "model_limits"); err == nil {
		t.Fatal("ambiguous trimmed names accepted")
	}
}
