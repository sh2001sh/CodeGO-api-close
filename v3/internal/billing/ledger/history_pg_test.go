//go:build pgintegration

package ledger

import (
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestHistoricalMoneyRemainsQueryableWithoutRepostingAndWithoutOtherUsers(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 5000)
	_, err := pool.Exec(ctx, `INSERT INTO v3_billing.historical_accounts VALUES
	 ('own',$1,'user',7,'wallet','credit','active',9,'{}','2026-09-01','2026-09-01'),
	 ('foreign',NULL,'user',8,'wallet','credit','active',1,'{}','2026-09-01','2026-09-01')`, account)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO v3_billing.historical_entries VALUES
	 ('same-time-a','own','','','usage','debit',-9007199254740993,NULL,'hist-a','usage','','system','','{}','2026-09-02'),
	 ('same-time-b','own','','','refund','credit',9007199254740993,5000,'hist-b','refund','','system','','{}','2026-09-02'),
	 ('foreign-entry','foreign','','','usage','debit',-10,90,'hist-f','usage','','system','','{}','2026-09-03')`)
	if err != nil {
		t.Fatal(err)
	}
	page, err := ReadHistoryWithArchive(ctx, pool, nil, 7, "", "", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "same-time-b" || page.Items[0].Amount != credits.Micro(9007199254740993) || page.Before == "" {
		t.Fatalf("first history page: %+v, %v", page, err)
	}
	second, err := ReadHistory(ctx, pool, 7, "", page.Before, 1)
	if err != nil || len(second.Items) != 1 || second.Items[0].ID != "same-time-a" || second.Items[0].Amount != credits.Micro(-9007199254740993) || second.Items[0].BalanceAfter != nil || second.Before != "" {
		t.Fatalf("second history page: %+v, %v", second, err)
	}
	foreign, err := ReadHistory(ctx, pool, 7, "foreign", "", 200)
	if err != nil || len(foreign.Items) != 0 {
		t.Fatalf("foreign historical account leaked: %+v, %v", foreign, err)
	}
	if _, err = ReadHistory(ctx, pool, 7, "", "corrupt", 50); !errors.Is(err, ErrHistoryQuery) {
		t.Fatalf("bad cursor accepted: %v", err)
	}
	if balance, version := pgBalance(t, pool, account); balance != 5000 || version != 0 {
		t.Fatalf("reading/importing history changed current money: %d/%d", balance, version)
	}
	for _, table := range []string{"ledger_entries", "balance_outbox"} {
		if count(t, pool, table) != 0 {
			t.Fatalf("history generated current %s", table)
		}
	}
}

func TestHistoricalMappedKeyAccountRequiresKeyOwner(t *testing.T) {
	pool := testPool(t)
	_, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(7,'history-owner'),(8,'other-owner');
	 INSERT INTO v3_identity.api_keys(id,user_id,key_hash,key_prefix,key_ciphertext) VALUES(70,7,decode(repeat('11',32),'hex'),'history-key',decode('11','hex'));
	 INSERT INTO v3_billing.accounts(id,owner_type,owner_id,kind) OVERRIDING SYSTEM VALUE VALUES(70,'api_key',70,'key_budget');
	 INSERT INTO v3_billing.historical_accounts VALUES('old-token',70,'token',70,'token','credit','active',1,'{}','2026-09-01','2026-09-01');
	 INSERT INTO v3_billing.historical_entries VALUES('token-entry','old-token','','','usage','debit',-10,NULL,'hist-token','usage','','system','','{}','2026-09-02')`)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []int64{7, 8} {
		page, err := ReadHistory(ctx, pool, user, "", "", 50)
		want := 0
		if user == 7 {
			want = 1
		}
		if err != nil || len(page.Items) != want {
			t.Fatalf("user %d mapped key history=%+v %v", user, page, err)
		}
	}
}
