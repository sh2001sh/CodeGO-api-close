//go:build pgintegration

package legacy

import (
	"context"
	"testing"
)

func TestRetiredPointsExcludedWithoutChangingCurrentWallet(t *testing.T) {
	source, target, crypto := importTestDB(t)
	ctx := context.Background()
	_, err := source.Exec(ctx, `UPDATE migration_source.users SET quota=9223372036854775807;
		CREATE TABLE migration_source.point_accounts(id bigint,user_id bigint,balance bigint,frozen_balance bigint);
		INSERT INTO migration_source.point_accounts VALUES(1,7,500,25);
		CREATE TABLE migration_source.user_pets(id bigint,user_id bigint);
		INSERT INTO migration_source.user_pets VALUES(3,7);
		INSERT INTO billing.accounts VALUES('old-gpt','user',7,'wallet','quota');
		INSERT INTO billing.balance_snapshots VALUES('old-gpt',9223372036854775807,100);`)
	if err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(readonlySource(t, source), target, crypto)
	report, err := importer.Import(ctx, true)
	if err != nil || !report.Applied || report.OpeningMicroCredits != "1000" || len(report.Issues) != 0 {
		t.Fatalf("retired data blocked current money: %+v err=%v", report, err)
	}
	if report.Counts["retired_features.point_accounts"] != 1 || report.Counts["retired_features.user_pets"] != 1 ||
		report.Amounts["retired_features.users.gpt_wallet_v2_units"] != "9223372036854775807" || report.Amounts["retired_features.point_accounts.balance_v2_units"] != "500" {
		t.Fatalf("missing retired source evidence: %+v", report)
	}
	var balance, accounts int64
	if err = target.QueryRow(ctx, `SELECT sum(balance),count(*) FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7`).Scan(&balance, &accounts); err != nil || balance != 1000 || accounts != 1 {
		t.Fatalf("points became money: balance=%d accounts=%d err=%v", balance, accounts, err)
	}
	if _, err = importer.Check(ctx); err != nil {
		t.Fatal(err)
	}
}
