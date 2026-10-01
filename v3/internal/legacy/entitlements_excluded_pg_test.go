//go:build pgintegration

package legacy

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestEntitlementsOnceOnlyAuditExclusionsIndependentPG(t *testing.T) {
	source, target, crypto := importTestDB(t)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `CREATE TABLE migration_source.unified_credit_user_migrations(id bigint,user_id bigint,status text,legacy_gpt_quota bigint,converted_unified_quota bigint,subscription_unified_quota bigint);
		INSERT INTO migration_source.unified_credit_user_migrations VALUES(1,999999,'obsolete-unknown-status',9223372036854775807,25,0);
		CREATE TABLE migration_source.subscription_tier_settlements(id bigint,user_subscription_id bigint,status text,amount_total bigint,amount_used bigint,unused_amount bigint,settlement_quota bigint);
		INSERT INTO migration_source.subscription_tier_settlements VALUES(1,999999,'obsolete-unknown-status',-15,2,99,9223372036854775807);
		CREATE TABLE migration_source.unified_credit_group_ratio_migrations(id bigint,version text,group_name text,ratio_before text,ratio_after text);
		INSERT INTO migration_source.unified_credit_group_ratio_migrations VALUES(1,'old','old','invalid-obsolete-ratio','invalid-obsolete-ratio');`); err != nil {
		t.Fatal(err)
	}
	reader := readonlySource(t, source)
	if _, err := reader.Exec(ctx, `UPDATE migration_source.unified_credit_user_migrations SET legacy_gpt_quota=0`); err == nil {
		t.Fatal("source reader unexpectedly has write permission")
	}
	snapshot, err := reader.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snapshot.Rollback(ctx) }()
	sources, err := discoverSources(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	data, err := loadEntitlements(ctx, snapshot, sources)
	if err != nil {
		t.Fatal(err)
	}
	users, err := loadUsers(ctx, snapshot, sources)
	if err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(reader, target, crypto)
	for i := 0; i < 2; i++ {
		r := Report{}
		data.validate(&r)
		if len(r.Issues) != 0 ||
			r.Counts["retired_features.unified_credit_user_migrations"] != 1 ||
			r.Counts["retired_features.subscription_tier_settlements"] != 1 ||
			r.Counts["retired_features.unified_credit_group_ratio_migrations"] != 1 ||
			r.Amounts["retired_features.unified_credit_user_migrations.legacy_gpt_quota_v2_units"] != "9223372036854775807" ||
			r.Amounts["retired_features.subscription_tier_settlements.amount_total_v2_units"] != "-15" {
			t.Fatalf("once-only audit exclusions=%+v", r)
		}
		if err = pgx.BeginFunc(ctx, target, func(targetTx pgx.Tx) error {
			if err := importer.importUsers(ctx, targetTx, users); err != nil {
				return err
			}
			return importer.importEntitlements(ctx, targetTx, data)
		}); err != nil {
			t.Fatal(err)
		}
	}
	r := Report{}
	err = pgx.BeginTxFunc(ctx, target, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(targetTx pgx.Tx) error {
		return importer.checkEntitlements(ctx, targetTx, data, &r)
	})
	if err != nil || len(r.Issues) != 0 || r.Counts["check:entitlements"] != 0 {
		t.Fatalf("check should not demand native audit rows: %+v %v", r, err)
	}
	var balance int64
	if err = target.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'`).Scan(&balance); err != nil || balance != 1000 {
		t.Fatalf("obsolete audit units monetized: %d %v", balance, err)
	}
	var retained int64
	if err = source.QueryRow(ctx, `SELECT legacy_gpt_quota FROM migration_source.unified_credit_user_migrations WHERE id=1`).Scan(&retained); err != nil || retained != 9223372036854775807 {
		t.Fatalf("source backup audit changed: %d %v", retained, err)
	}
}
