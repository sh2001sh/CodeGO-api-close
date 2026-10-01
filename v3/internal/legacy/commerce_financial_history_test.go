package legacy

import (
	"encoding/json"
	"testing"
)

func TestCommercePaymentIdentityKeepsMerchantAndProviderTransactionsDistinct(t *testing.T) {
	d := commerceTestData(t)
	for _, test := range []struct{ name, provider, external, payload, reference, event string }{
		{"top_ups", "epay", "remote-payment", `{}`, "merchant", "remote-payment"},
		{"top_ups", "xunhu", "xunhu-payment", `{}`, "merchant", "xunhu-payment"},
		{"top_ups", "stripe", "checkout-session", `{}`, "checkout-session", "checkout-session"},
		{"top_ups", "creem", "checkout-session", `{}`, "checkout-session", "checkout-session"},
		{"top_ups", "epay", "", `{"trade_no":"verified-payment"}`, "merchant", "verified-payment"},
		{"subscription_orders", "epay", "", `{"TradeNo":"verified-sub-payment","ServiceTradeNo":"merchant","VerifyStatus":true}`, "merchant", "verified-sub-payment"},
		{"subscription_orders", "epay", "", `{"out_trade_no":"merchant"}`, "merchant", ""},
		{"subscription_orders", "epay", "", `{}`, "merchant", ""},
	} {
		r := commerceTestRow(t, `{"id":1,"user_id":7,"plan_id":5,"amount":1,"money":1,"trade_no":"merchant","status":"success","create_time":10,"complete_time":20}`)
		r["payment_provider"] = []byte(`"` + test.provider + `"`)
		r["external_payment_id"] = []byte(`"` + test.external + `"`)
		payload, err := json.Marshal(test.payload)
		if err != nil {
			t.Fatal(err)
		}
		r["provider_payload"] = payload
		p, err := d.project(test.name, r)
		if err != nil || p.values["provider_reference"] != test.reference || (test.event != "" && p.values["payment_event_id"] != test.event) || (test.event == "" && p.values["payment_event_id"] != nil) {
			t.Fatalf("payment source=%+v projected=%+v err=%v", test, p.values, err)
		}
	}
}

func TestCommerceMissingProviderTransactionIsReportedWithoutInventingAnID(t *testing.T) {
	d := commerceTestData(t)
	d.rows["subscription_orders"] = []commerceRow{commerceTestRow(t, `{"id":1,"user_id":7,"plan_id":5,"money":1,"trade_no":"merchant-only","status":"success","payment_provider":"epay","create_time":10,"complete_time":20,"provider_payload":"{\"out_trade_no\":\"merchant-only\"}"}`)}
	report := Report{}
	d.validate(&report)
	if len(report.Issues) != 0 || report.Counts["orders_missing_provider_transaction"] != 1 {
		t.Fatalf("missing original transaction not reported=%+v", report)
	}
}

func TestCommerceResetHistoryRetainsLifetimeUseOnly(t *testing.T) {
	rows := []commerceRow{
		commerceTestRow(t, `{"related_user_id":9,"change_type":"use","created_at":1}`),
		commerceTestRow(t, `{"related_user_id":10,"change_type":"earn","created_at":20}`),
		commerceTestRow(t, `{"related_user_id":999,"change_type":"use","created_at":1}`),
	}
	used, err := commerceResetUsed(rows)
	if err != nil || !used[9] || used[10] || used[11] {
		t.Fatalf("reset history=%v err=%v", used, err)
	}
	d := commerceTestData(t)
	d.resetUsed = used
	sub := commerceTestRow(t, `{"id":9,"user_id":7,"plan_id":5,"amount_total":1000,"status":"active","start_time":10,"end_time":20}`)
	p, err := d.project("user_subscriptions", sub)
	if err != nil || p.values["reset_opportunity_used"] != true {
		t.Fatalf("lifetime guard missing=%+v err=%v", p.values, err)
	}
}
