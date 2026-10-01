package legacy

import (
	"math"
	"math/big"
	"testing"
)

func TestCommerceRenewableIncludesOnlyVerifiedCurrentCycleGroupRewards(t *testing.T) {
	sub := commerceTestRow(t, `{"id":9,"user_id":7,"start_time":1700000000}`)
	member := func(id int, usd, created, granted string) commerceRow {
		row := commerceTestRow(t, `{"id":1,"user_id":7,"user_subscription_id":9,"bonus_amount_usd":`+usd+`,"created_at":`+created+`,"bonus_granted":`+granted+`}`)
		row["id"] = []byte(big.NewInt(int64(id)).String())
		return row
	}
	account := commerceTestRow(t, `{"account_id":"sub9","owner_id":9,"owner_type":"user_subscription","account_type":"subscription","quota_unit":"quota"}`)
	grant := func(key, created, reason, account, amount string) commerceRow {
		return commerceTestRow(t, `{"account_id":"`+account+`","idempotency_key":"`+key+`","reason_code":"`+reason+`","created_at":"`+created+`","amount":`+amount+`}`)
	}
	members := []commerceRow{
		member(1, "0.000201", "1700000000", "true"), // old units round101, then ×2=202 (not201).
		member(2, "1", "1699999999", "true"),
		member(3, "1", "1699999999", "true"),
		member(4, "1", "1700000001", "false"),
		member(5, "1", "1700000101", "true"),
		member(6, "0", "1700000001", "true"),
		member(7, "0.0000001", "1699999999", "true"),
	}
	grants := []commerceRow{
		grant("group-buy:1:member:1:tier:101", "2023-11-14T22:13:20Z", "subscription_bonus", "sub9", "101"), // current member already counted once.
		grant("group-buy:1:member:2:tier:37", "2023-11-14T22:15:00.999999Z", "subscription_bonus", "sub9", "37"),
		grant("group-buy:1:member:2:tier:38", "2023-11-14T22:15:01Z", "subscription_bonus", "sub9", "38"), // outside exclusive next-second bound.
		grant("group-buy:1:member:3:tier:10", "2023-11-14T22:13:19Z", "subscription_bonus", "sub9", "10"),
		grant("group-buy:1:member:7:tier:2", "2023-11-14T22:13:20Z", "subscription_bonus", "sub9", "2"),
		grant("group-buy:1:member:2:tier:99", "2023-11-14T22:13:20Z", "subscription_fuel", "sub9", "99"),
		grant("group-buy:1:member:22:tier:99", "2023-11-14T22:13:20Z", "subscription_bonus", "sub9", "99"),
		grant("group-buy:1:member:2:tier:99", "2023-11-14T22:13:20Z", "subscription_bonus", "foreign", "99"),
	}
	bonuses, err := commerceCycleBonuses([]commerceRow{sub}, members, []commerceRow{account}, grants, 1700000100, "500000")
	if err != nil || bonuses[9] == nil || bonuses[9].Int64() != 280 {
		t.Fatalf("group renewable bonus=%v err=%v", bonuses[9], err)
	}
	d := &commerceData{renewableBonuses: bonuses}
	if actual := d.subscriptionRenewable(9, 2000, 1000); actual != 1280 {
		t.Fatalf("fuel became renewable or group reward lost: %d", actual)
	}
	if actual := d.subscriptionRenewable(9, 1100, 1000); actual != 1100 {
		t.Fatalf("group bonus exceeded original total: %d", actual)
	}
	if actual := d.subscriptionRenewable(9, 0, 1000); actual != 0 {
		t.Fatalf("unlimited source changed its renewal semantics: %d", actual)
	}
}

func TestCommerceRenewableRejectsForeignOwnerAndUsesSafeClampedTotals(t *testing.T) {
	sub := commerceTestRow(t, `{"id":9,"user_id":7,"start_time":10}`)
	member := commerceTestRow(t, `{"id":1,"user_id":8,"user_subscription_id":9,"bonus_amount_usd":1,"created_at":10,"bonus_granted":true}`)
	if _, err := commerceCycleBonuses([]commerceRow{sub}, []commerceRow{member}, nil, nil, 20, "500000"); err == nil {
		t.Fatal("foreign membership changed subscription renewal allowance")
	}
	d := &commerceData{renewableBonuses: map[int64]*big.Int{9: new(big.Int).Lsh(big.NewInt(1), 100)}}
	if actual := d.subscriptionRenewable(9, math.MaxInt64, math.MaxInt64-1); actual != math.MaxInt64 {
		t.Fatalf("unbounded aggregate overflowed clamped allowance: %d", actual)
	}
	if actual := d.subscriptionRenewable(10, 100, 200); actual != 100 {
		t.Fatalf("no verified group grant must retain min(total,base): %d", actual)
	}
}

func TestCommerceRenewableUsesSourceQuotaPerUnitAndRejectsInvalidOptions(t *testing.T) {
	sub := commerceTestRow(t, `{"id":9,"user_id":7,"start_time":10}`)
	member := commerceTestRow(t, `{"id":1,"user_id":7,"user_subscription_id":9,"bonus_amount_usd":0.000002,"created_at":10,"bonus_granted":true}`)
	for _, test := range []struct {
		config string
		want   int64
	}{{"250000", 2}, {"125000", 0}, {"5e5", 2}} {
		units, err := commerceRenewableQuotaPerUnit(map[string]string{"QuotaPerUnit": test.config})
		if err != nil {
			t.Fatal(err)
		}
		bonuses, err := commerceCycleBonuses([]commerceRow{sub}, []commerceRow{member}, nil, nil, 20, units)
		if err != nil || bonuses[9] == nil || bonuses[9].Int64() != test.want {
			t.Fatalf("configured=%s bonus=%v expected=%d err=%v", test.config, bonuses[9], test.want, err)
		}
	}
	for _, invalid := range []string{"", "0", "-1", "NaN", "Infinity", "1e309", "1e-1000", "1/2"} {
		if _, err := commerceRenewableQuotaPerUnit(map[string]string{"QuotaPerUnit": invalid}); err == nil {
			t.Fatalf("invalid source QuotaPerUnit accepted: %q", invalid)
		}
	}
	if units, err := commerceRenewableQuotaPerUnit(nil); err != nil || units != "500000" {
		t.Fatalf("original source default changed: %s %v", units, err)
	}
}
