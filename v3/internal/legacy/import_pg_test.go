//go:build pgintegration

package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/migrations"
)

func importTestDB(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool, *catalog.AESGCM) {
	t.Helper()
	dsn := os.Getenv("V3_MIGRATION_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_MIGRATION_TEST_PG_DSN not set; use a disposable dedicated database")
	}
	ctx := context.Background()
	sourcePool := migrationDatabase(t, dsn, "source")
	pool := migrationDatabase(t, dsn, "target")
	var err error
	names, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		source, readErr := migrations.Read(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err = pool.Exec(ctx, source); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	_, err = sourcePool.Exec(ctx, `CREATE SCHEMA migration_source; CREATE SCHEMA billing;
		CREATE TABLE migration_source.users(id bigint PRIMARY KEY,username text,password text,role int,status int,"group" text,
			quota bigint,claude_quota bigint,setting text);
		CREATE TABLE migration_source.tokens(id bigint,user_id bigint,key text,status int,remain_quota bigint,unlimited_quota bool,allow_ips text);
		CREATE TABLE migration_source.channels(id bigint,type int,name text,key text,status int,"group" text,models text,base_url text);
		CREATE TABLE migration_source.options(key text PRIMARY KEY,value text);
		CREATE TABLE billing.accounts(account_id text PRIMARY KEY,owner_type text,owner_id bigint,account_type text,quota_unit text);
		CREATE TABLE billing.balance_snapshots(account_id text,available_balance bigint,reserved_balance bigint);
		INSERT INTO migration_source.users VALUES(7,'alice','bcrypt-placeholder',1,1,'default',0,500,'{}');
		INSERT INTO migration_source.tokens VALUES(11,7,'kept-existing-key',1,100,true,'127.0.0.1');
		INSERT INTO migration_source.channels VALUES(13,1,'upstream','secret,with,commas',1,'default','chat-model','https://example.invalid');
		INSERT INTO migration_source.options VALUES('ModelRatio','{"chat-model":1.5}'),('GroupRatio','{"default":1.2}'),('EPayKey','local-test-secret');
		INSERT INTO billing.accounts VALUES('wallet-7','user',7,'claude_wallet','quota');
		INSERT INTO billing.balance_snapshots VALUES('wallet-7',500,0);`)
	if err != nil {
		t.Fatal(err)
	}
	crypto, err := catalog.NewAESGCM(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return sourcePool, pool, crypto
}

func TestOfflineImportDryRunAtomicAndIdempotent(t *testing.T) {
	sourcePool, pool, crypto := importTestDB(t)
	ctx := context.Background()
	reader := readonlySource(t, sourcePool)
	if _, err := reader.Exec(ctx, `UPDATE migration_source.users SET claude_quota=0`); err == nil {
		t.Fatal("read-only source role can write")
	}
	importer := NewImporter(reader, pool, crypto)
	report, err := importer.Import(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Users != 1 || report.Keys != 1 || report.Channels != 1 || report.OpeningMicroCredits != "1000" || len(report.Issues) != 0 || report.Applied {
		t.Fatalf("dry-run=%+v", report)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("dry-run wrote users: %d %v", count, err)
	}
	for i := 0; i < 2; i++ {
		if report, err = importer.Import(ctx, true); err != nil || !report.Applied {
			t.Fatalf("apply %d: %+v %v", i, report, err)
		}
	}
	var balance, entryCount int64
	if err = pool.QueryRow(ctx, `SELECT balance,(SELECT count(*) FROM v3_billing.ledger_entries) FROM v3_billing.accounts
		WHERE owner_type='user' AND owner_id=7 AND kind='wallet'`).Scan(&balance, &entryCount); err != nil {
		t.Fatal(err)
	}
	if balance != 1000 || entryCount != 1 {
		t.Fatalf("balance=%d entries=%d", balance, entryCount)
	}
	var hash, ciphertext []byte
	if err = pool.QueryRow(ctx, `SELECT key_hash,key_ciphertext FROM v3_identity.api_keys WHERE id=11`).Scan(&hash, &ciphertext); err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256([]byte("sk-kept-existing-key"))
	if string(hash) != string(expected[:]) {
		t.Fatal("existing API key hash changed")
	}
	plaintext, err := crypto.Decrypt(ciphertext)
	if err != nil || string(plaintext) != "sk-kept-existing-key" {
		t.Fatal("existing API key failed decryption")
	}
	control, err := identity.NewControl(pool, identity.ControlConfig{SessionSecret: make([]byte, 32), EncryptionKey: make([]byte, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if revealed, revealErr := control.RevealKey(ctx, 7, 11); revealErr != nil || revealed != "sk-kept-existing-key" {
		t.Fatalf("migrated key unavailable from control: %v", revealErr)
	}
	if _, revealErr := control.RevealKey(ctx, 8, 11); !errors.Is(revealErr, identity.ErrNotFound) {
		t.Fatal("migrated key ownership bypass")
	}
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_catalog.channel_credentials WHERE channel_id=13),secret
		FROM v3_catalog.channel_credentials WHERE channel_id=13 LIMIT 1`).Scan(&count, &ciphertext); err != nil {
		t.Fatal(err)
	}
	plaintext, err = crypto.Decrypt(ciphertext)
	if count != 1 || err != nil || string(plaintext) != "secret,with,commas" {
		t.Fatal("channel credential splitting or replay failed")
	}
	var originalBalance int64
	if err = sourcePool.QueryRow(ctx, `SELECT claude_quota FROM migration_source.users WHERE id=7`).Scan(&originalBalance); err != nil || originalBalance != 500 {
		t.Fatalf("source mutated: balance=%d err=%v", originalBalance, err)
	}
}

func TestOfflineImportBlocksWalletMismatchAndRollsBack(t *testing.T) {
	sourcePool, pool, crypto := importTestDB(t)
	ctx := context.Background()
	if _, err := sourcePool.Exec(ctx, `UPDATE migration_source.users SET claude_quota=499`); err != nil {
		t.Fatal(err)
	}
	report, err := NewImporter(sourcePool, pool, crypto).Import(ctx, true)
	if err == nil || report.Applied || len(report.Issues) != 1 || report.Issues[0].Code != "wallet_projection_mismatch" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	encoded, _ := json.Marshal(report)
	if string(encoded) == "" {
		t.Fatal("report unavailable")
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed import wrote users: %d %v", count, err)
	}
}

func TestOfflineImportRejectsInvalidSubscriptionData(t *testing.T) {
	sourcePool, pool, crypto := importTestDB(t)
	ctx := context.Background()
	if _, err := sourcePool.Exec(ctx, `CREATE TABLE migration_source.user_subscriptions(id bigint); INSERT INTO migration_source.user_subscriptions VALUES(9)`); err != nil {
		t.Fatal(err)
	}
	report, err := NewImporter(sourcePool, pool, crypto).Import(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) == 0 {
		t.Fatalf("invalid subscription accepted: %+v", report)
	}
	if _, err = NewImporter(sourcePool, pool, crypto).Import(ctx, true); err == nil {
		t.Fatal("silently imported without existing subscription data")
	}
}
