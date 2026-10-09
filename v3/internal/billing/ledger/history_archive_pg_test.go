//go:build pgintegration

package ledger

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func archiveFixture(t *testing.T, target *pgxpool.Pool) (source, reader *pgxpool.Pool, role string) {
	t.Helper()
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	name := "ledger_archive_" + hex.EncodeToString(nonce[:])
	role = "ledger_archive_read_" + hex.EncodeToString(nonce[:])
	if _, err := target.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	cfg := target.Config().Copy()
	cfg.ConnConfig.Database = name
	var err error
	source, err = pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.Close)
	_, err = source.Exec(ctx, `CREATE SCHEMA billing;
	 CREATE TABLE billing.ledger_entries(entry_id varchar(64) PRIMARY KEY,account_id varchar(64) NOT NULL,
	 amount bigint NOT NULL,balance_after bigint,entry_type varchar(32) NOT NULL,direction varchar(16) NOT NULL,
	 reason_code varchar(64) NOT NULL,created_at timestamptz NOT NULL);
	 CREATE INDEX ON billing.ledger_entries(account_id,created_at DESC,entry_id DESC);`)
	if err != nil {
		t.Fatal(err)
	}
	quotedRole := pgx.Identifier{role}.Sanitize()
	_, err = source.Exec(ctx, `CREATE ROLE `+quotedRole+` LOGIN PASSWORD 'local-ledger-archive-test';
	 GRANT USAGE ON SCHEMA billing TO `+quotedRole+`;
	 GRANT SELECT(entry_id,account_id,amount,balance_after,entry_type,direction,reason_code,created_at) ON billing.ledger_entries TO `+quotedRole+`;
	 GRANT EXECUTE ON FUNCTION pg_catalog.pg_control_system() TO `+quotedRole)
	if err != nil {
		t.Fatal(err)
	}
	cfg = source.Config().Copy()
	cfg.ConnConfig.User, cfg.ConnConfig.Password = role, "local-ledger-archive-test"
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	reader, err = pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	var cluster, database string
	var oid int64
	if err = source.QueryRow(ctx, `SELECT c.system_identifier::text,d.oid::bigint,current_database()
	 FROM pg_control_system() c JOIN pg_database d ON d.datname=current_database()`).Scan(&cluster, &oid, &database); err != nil {
		t.Fatal(err)
	}
	if _, err = target.Exec(ctx, `INSERT INTO v3_billing.ledger_history_archive(singleton,source_cluster_id,source_database_oid,source_database,entry_count,archived_at)
	 VALUES(true,$1,$2,$3,4,now())`, cluster, oid, database); err != nil {
		t.Fatal(err)
	}
	return source, reader, role
}

func TestArchiveHistoryUsesCurrentWalletKeyAndSubscriptionOwnershipAndGlobalCursor(t *testing.T) {
	target := testPool(t)
	wallet := fundedAccount(t, target, 7, 5000)
	_, err := target.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(7,'history-owner'),(8,'foreign-owner');
	 INSERT INTO v3_identity.api_keys(id,user_id,key_hash,key_prefix,key_ciphertext) VALUES(70,7,decode(repeat('11',32),'hex'),'history-key',decode('11','hex'));
	 INSERT INTO v3_billing.accounts(id,owner_type,owner_id,kind) OVERRIDING SYSTEM VALUE VALUES(70,'api_key',70,'key_budget'),(71,'subscription',99,'subscription');
	 INSERT INTO v3_commerce.plans(id,name,price_minor,credits,period_seconds) VALUES(1,'Historical plan',100,1000,3600);
	 INSERT INTO v3_commerce.subscriptions(id,user_id,plan_id,account_id,starts_at,expires_at,state) VALUES(99,7,1,71,'2024-01-01','2024-01-02','expired')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = target.Exec(ctx, `INSERT INTO v3_billing.historical_accounts VALUES
	 ('wallet-source',$1,'user',7,'claude_wallet','credit','active',0,'{}','2024-01-01','2024-01-01'),
	 ('key-source',70,'token',70,'token','credit','active',0,'{}','2024-01-01','2024-01-01'),
	 ('subscription-source',71,'user_subscription',99,'subscription','credit','closed',0,'{}','2024-01-01','2024-01-01'),
	 ('foreign-source',NULL,'user',8,'claude_wallet','credit','active',0,'{}','2024-01-01','2024-01-01')`, wallet)
	if err != nil {
		t.Fatal(err)
	}
	source, reader, _ := archiveFixture(t, target)
	_, err = source.Exec(ctx, `INSERT INTO billing.ledger_entries VALUES
	 ('c','wallet-source',9007199254740993,NULL,'usage','debit','usage','2024-01-01'),
	 ('b','key-source',20,100,'refund','credit','refund','2024-01-01'),
	 ('a','subscription-source',5,50,'usage','debit','usage','2024-01-01'),
	 ('foreign-entry','foreign-source',999,999,'refund','credit','refund','2024-01-02')`)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateHistoryArchive(ctx, target, reader); err != nil {
		t.Fatal(err)
	}
	var before string
	for index, id := range []string{"c", "b", "a"} {
		page, err := ReadHistoryWithArchive(ctx, target, reader, 7, "", before, 1)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != id || (page.Before != "") != (index < 2) {
			t.Fatalf("global page %d: %+v err=%v", index, page, err)
		}
		if index == 0 && (page.Items[0].Amount != -18014398509481986 || page.Items[0].BalanceAfter != nil) {
			t.Fatalf("exact source debit/null lost: %+v", page)
		}
		if index == 1 && (page.Items[0].Amount != 40 || page.Items[0].BalanceAfter == nil || *page.Items[0].BalanceAfter != 200) {
			t.Fatalf("source credit/balance conversion lost: %+v", page)
		}
		before = page.Before
	}
	for _, test := range []struct {
		user   int64
		source string
		want   int
	}{{7, "foreign-source", 0}, {8, "key-source", 0}, {8, "subscription-source", 0}, {7, "subscription-source", 1}, {7, "key-source", 1}} {
		page, err := ReadHistoryWithArchive(ctx, target, reader, test.user, test.source, "", 200)
		if err != nil || len(page.Items) != test.want {
			t.Fatalf("account visibility %+v: %+v err=%v", test, page, err)
		}
	}
	if _, err := target.Exec(ctx, `UPDATE v3_identity.api_keys SET user_id=8 WHERE id=70`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		user int64
		want int
	}{{7, 0}, {8, 1}} {
		page, err := ReadHistoryWithArchive(ctx, target, reader, test.user, "key-source", "", 50)
		if err != nil || len(page.Items) != test.want {
			t.Fatalf("stale source key ownership used for user %d: %+v err=%v", test.user, page, err)
		}
	}
	if balance, version := pgBalance(t, target, wallet); balance != 5000 || version != 0 {
		t.Fatalf("archive read changed current money: %d/%d", balance, version)
	}
	for _, table := range []string{"ledger_entries", "historical_entries", "balance_outbox"} {
		if count(t, target, table) != 0 {
			t.Fatalf("archive copied/reposted %s", table)
		}
	}
	if _, err := source.Exec(ctx, `INSERT INTO billing.ledger_entries VALUES('overflow','wallet-source',9223372036854775807,NULL,'refund','credit','refund','2024-01-03')`); err != nil {
		t.Fatal(err)
	}
	if page, err := ReadHistoryWithArchive(ctx, target, reader, 7, "", "", 50); !errors.Is(err, ErrHistoryArchive) || len(page.Items) != 0 {
		t.Fatalf("corrupt amount became a successful/partial page: %+v err=%v", page, err)
	}
	if _, err := source.Exec(ctx, `UPDATE billing.ledger_entries SET amount=1,created_at='0001-01-01T00:00:00Z' WHERE entry_id='overflow'`); err != nil {
		t.Fatal(err)
	}
	if page, err := ReadHistoryWithArchive(ctx, target, reader, 7, "", "", 50); !errors.Is(err, ErrHistoryArchive) || len(page.Items) != 0 {
		t.Fatalf("UTC zero time became a successful/invalid-cursor page: %+v err=%v", page, err)
	}
}

func TestArchiveConfigurationPermissionsAndSourceFailuresAreNeverEmptySuccess(t *testing.T) {
	target := testPool(t)
	source, reader, role := archiveFixture(t, target)
	assertFailure := func(t *testing.T, archive *pgxpool.Pool) {
		t.Helper()
		if err := ValidateHistoryArchive(ctx, target, archive); !errors.Is(err, ErrHistoryArchive) {
			t.Fatalf("startup accepted invalid archive: %v", err)
		}
		// This user has no historical accounts. Archive failure must still be an error.
		if page, err := ReadHistoryWithArchive(ctx, target, archive, 7, "", "", 50); !errors.Is(err, ErrHistoryArchive) {
			t.Fatalf("source failure became successful empty history: %+v %v", page, err)
		}
	}
	t.Run("missing DSN", func(t *testing.T) { assertFailure(t, nil) })
	t.Run("admin role", func(t *testing.T) { assertFailure(t, source) })
	t.Run("wrong database", func(t *testing.T) {
		if _, err := target.Exec(ctx, `UPDATE v3_billing.ledger_history_archive SET source_database_oid=source_database_oid+1`); err != nil {
			t.Fatal(err)
		}
		assertFailure(t, reader)
		if _, err := target.Exec(ctx, `UPDATE v3_billing.ledger_history_archive SET source_database_oid=source_database_oid-1`); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("wrong cluster", func(t *testing.T) {
		if _, err := target.Exec(ctx, `UPDATE v3_billing.ledger_history_archive SET source_cluster_id='wrong-'||source_cluster_id`); err != nil {
			t.Fatal(err)
		}
		assertFailure(t, reader)
		if _, err := target.Exec(ctx, `UPDATE v3_billing.ledger_history_archive SET source_cluster_id=substring(source_cluster_id FROM 7)`); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("row policy cannot silently hide evidence", func(t *testing.T) {
		if _, err := source.Exec(ctx, `ALTER TABLE billing.ledger_entries ENABLE ROW LEVEL SECURITY`); err != nil {
			t.Fatal(err)
		}
		assertFailure(t, reader)
		if _, err := source.Exec(ctx, `ALTER TABLE billing.ledger_entries DISABLE ROW LEVEL SECURITY`); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("writable column", func(t *testing.T) {
		quoted := pgx.Identifier{role}.Sanitize()
		if _, err := source.Exec(ctx, `GRANT UPDATE(amount) ON billing.ledger_entries TO `+quoted); err != nil {
			t.Fatal(err)
		}
		assertFailure(t, reader)
		if _, err := source.Exec(ctx, `REVOKE UPDATE(amount) ON billing.ledger_entries FROM `+quoted); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("missing column permission", func(t *testing.T) {
		quoted := pgx.Identifier{role}.Sanitize()
		if _, err := source.Exec(ctx, `REVOKE SELECT(balance_after) ON billing.ledger_entries FROM `+quoted); err != nil {
			t.Fatal(err)
		}
		assertFailure(t, reader)
		if _, err := source.Exec(ctx, `GRANT SELECT(balance_after) ON billing.ledger_entries TO `+quoted); err != nil {
			t.Fatal(err)
		}
	})
	if err := ValidateHistoryArchive(ctx, target, reader); err != nil {
		t.Fatalf("valid source rejected after permissions restored: %v", err)
	}
	t.Run("closed archive", func(t *testing.T) {
		reader.Close()
		assertFailure(t, reader)
	})
}
