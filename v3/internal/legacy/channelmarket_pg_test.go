//go:build pgintegration

package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestChannelMarketSeparateDatabasesIdempotentAndRuntimeReadable(t *testing.T) {
	source, target, crypto := marketMigrationDBs(t)
	ctx := context.Background()
	sources := seedChannelMarketFixture(t, source)
	if _, err := source.Exec(ctx, `CREATE TABLE public.cm_users(id bigint);INSERT INTO public.cm_users VALUES(7),(8);CREATE TABLE public.cm_channels(id bigint,"group" text);INSERT INTO public.cm_channels VALUES(13,'default')`); err != nil {
		t.Fatal(err)
	}
	sources["users"] = `public.cm_users`
	sources["channels"] = `public.cm_channels`
	if _, err := target.Exec(ctx, `INSERT INTO v3_identity.users(id,username,role,status) VALUES(7,'owner','user','active'),(8,'consumer','user','active');INSERT INTO v3_catalog.groups(name) VALUES('default');INSERT INTO v3_catalog.channels(id,name,provider,base_url) VALUES(13,'core','openai','https://example.invalid');INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES(13,'default');INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES(13,'chat-model')`); err != nil {
		t.Fatal(err)
	}
	load := func() *channelMarketData {
		tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
		d, err := loadChannelMarket(ctx, tx, sources)
		if err != nil {
			t.Fatal(err)
		}
		report := Report{}
		d.validate(&report)
		if len(report.Issues) != 0 {
			t.Fatalf("issues=%+v", report.Issues)
		}
		return d
	}
	data := load()
	var before int
	if err := target.QueryRow(ctx, `SELECT count(*) FROM v3_channelmarket.groups`).Scan(&before); err != nil || before != 0 {
		t.Fatal("preview wrote target")
	}
	importer := NewImporter(source, target, crypto)
	for i := 0; i < 2; i++ {
		tx, err := target.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = importer.importChannelMarket(ctx, tx, data); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	assertMigratedMarketCardFlags(t, target, importer, data)
	assertMigratedMarketPriceQuantum(t, target, importer, data)
	var publicID string
	var ppm, pending, gross, ratingID, expiryCount, memberCount int64
	if err := target.QueryRow(ctx, `SELECT public_channel_id,multiplier_ppm FROM v3_channelmarket.groups WHERE id='legacy-group-201'`).Scan(&publicID, &ppm); err != nil {
		t.Fatal(err)
	}
	if err := target.QueryRow(ctx, `SELECT (SELECT balance FROM v3_billing.accounts WHERE owner_id=7 AND kind='marketplace_pending'),(SELECT gross_micro FROM v3_channelmarket.settlements WHERE id='settlement-201'),(SELECT legacy_id FROM v3_community.channel_ratings WHERE channel_id='legacy-public-201'),(SELECT count(*) FROM v3_channelmarket.group_invites WHERE id=91 AND expires_at IS NULL),(SELECT count(*) FROM v3_channelmarket.route_pool_members)`).Scan(&pending, &gross, &ratingID, &expiryCount, &memberCount); err != nil {
		t.Fatal(err)
	}
	if publicID != "legacy-public-201" || ppm != 75000 || pending != 190 || gross != 200 || ratingID != 98 || expiryCount != 1 || memberCount != 2 {
		t.Fatalf("mapping: id=%s ppm=%d pending=%d gross=%d rating=%d expiry=%d members=%d", publicID, ppm, pending, gross, ratingID, expiryCount, memberCount)
	}
	checkTx, err := target.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	checkReport := Report{}
	if err = importer.checkChannelMarket(ctx, checkTx, data, &checkReport); err != nil {
		_ = checkTx.Rollback(ctx)
		t.Fatal(err)
	}
	_ = checkTx.Rollback(ctx)
	if len(checkReport.Issues) != 0 || checkReport.Counts["check:channelmarket"] < 20 {
		t.Fatalf("check=%+v", checkReport)
	}
	service := channelmarket.New(target, crypto, nil, channelmarket.Config{}, nil)
	view, err := service.Get(ctx, channelmarket.Actor{UserID: 7}, publicID)
	if err != nil || view.ID != publicID || view.GroupID != "legacy-group-201" {
		t.Fatalf("runtime did not consume migrated channel: %+v %v", view, err)
	}
	if _, err = service.Get(ctx, channelmarket.Actor{UserID: 8}, publicID); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("source consumer block was not enforced after migration: %v", err)
	}
	if _, err = service.Get(ctx, channelmarket.Actor{UserID: 999}, publicID); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatal("private source channel became public after migration")
	}
	snapshotTx, err := target.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := catalog.ReadMarketSnapshot(ctx, snapshotTx)
	_ = snapshotTx.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	policy := snapshot.Channels[13]
	if !snapshot.Groups["fixture_market_internal"].Allows(8) || snapshot.Groups["fixture_market_internal"].Allows(999) || !policy.Blocked[8] || policy.Factor(8, time.Now()) != 80000 {
		t.Fatal("runtime did not consume migrated private access/block/custom price")
	}
	if _, err := source.Exec(ctx, `UPDATE marketplace.user_multipliers SET multiplier=0.09`); err != nil {
		t.Fatal(err)
	}
	changed := load()
	tx, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = importer.importChannelMarket(ctx, tx, changed)
	_ = tx.Rollback(ctx)
	if err == nil {
		t.Fatal("altered replay accepted")
	}
	if _, err = target.Exec(ctx, `UPDATE v3_channelmarket.settlements SET consumer_micro=consumer_micro+1 WHERE id='settlement-201';DELETE FROM v3_channelmarket.channel_feedback WHERE id=97`); err != nil {
		t.Fatal(err)
	}
	checkTx, err = target.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	checkReport = Report{}
	if err = importer.checkChannelMarket(ctx, checkTx, data, &checkReport); err != nil {
		_ = checkTx.Rollback(ctx)
		t.Fatal(err)
	}
	_ = checkTx.Rollback(ctx)
	if len(checkReport.Issues) < 2 {
		t.Fatalf("check missed changed money or missing row: %+v", checkReport)
	}
}

func TestChannelMarketSourceCipherAndPendingMismatch(t *testing.T) {
	// Cipher helper is also checked on genuine encoded input in a PG fixture.
	source, target, crypto := marketMigrationDBs(t)
	ctx := context.Background()
	sources := seedChannelMarketFixture(t, source)
	if _, err := source.Exec(ctx, `CREATE TABLE public.cm_users(id bigint);INSERT INTO public.cm_users VALUES(7),(8);CREATE TABLE public.cm_channels(id bigint,"group" text);INSERT INTO public.cm_channels VALUES(13,'default');CREATE TABLE public.cm_accounts(account_id text,owner_type text,owner_id bigint,account_type text);CREATE TABLE public.cm_snapshots(account_id text,available_balance bigint,reserved_balance bigint);INSERT INTO public.cm_accounts VALUES('pending7','user',7,'marketplace_owner_pending');INSERT INTO public.cm_snapshots VALUES('pending7',94,0)`); err != nil {
		t.Fatal(err)
	}
	sources["users"] = `public.cm_users`
	sources["channels"] = `public.cm_channels`
	sources["accounts"] = `public.cm_accounts`
	sources["balance_snapshots"] = `public.cm_snapshots`
	key := "fixture-source-secret"
	t.Setenv("V3_MIGRATION_SOURCE_CRYPTO_SECRET", key)
	digest := sha256.Sum256([]byte(key))
	sourceCrypto, err := catalog.NewAESGCM(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	urlCipher, _ := sourceCrypto.Encrypt([]byte("https://example.invalid"))
	credentialCipher, _ := sourceCrypto.Encrypt([]byte("fixture-upstream-secret"))
	encode := func(value []byte) string { return "enc:v1:" + base64.RawURLEncoding.EncodeToString(value) }
	if _, err = source.Exec(ctx, `ALTER TABLE marketplace.channels ADD COLUMN base_url_ciphertext text;ALTER TABLE marketplace.channels ADD COLUMN credential_ciphertext text`); err != nil {
		t.Fatal(err)
	}
	if _, err = source.Exec(ctx, `UPDATE marketplace.channels SET internal_channel_id=NULL,base_url_ciphertext=$1,credential_ciphertext=$2`, encode(urlCipher), encode(credentialCipher)); err != nil {
		t.Fatal(err)
	}
	load := func() *channelMarketData {
		tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
		data, err := loadChannelMarket(ctx, tx, sources)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	data := load()
	report := Report{}
	data.validate(&report)
	if len(report.Issues) != 1 || report.Issues[0].Code != "invalid_market_balance" {
		t.Fatalf("mismatch=%+v", report)
	}
	if _, err = source.Exec(ctx, `UPDATE public.cm_snapshots SET available_balance=95`); err != nil {
		t.Fatal(err)
	}
	data = load()
	if len(data.issues) != 0 {
		t.Fatalf("issues=%+v", data.issues)
	}
	if _, err = target.Exec(ctx, `INSERT INTO v3_identity.users(id,username,role,status) VALUES(7,'owner','user','active'),(8,'consumer','user','active');INSERT INTO v3_catalog.groups(name) VALUES('default');INSERT INTO v3_catalog.channels(id,name,provider) VALUES(13,'official','openai');INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES(13,'default')`); err != nil {
		t.Fatal(err)
	}
	tx, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = NewImporter(source, target, crypto).importChannelMarket(ctx, tx, data); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertMigratedMarketCardFlags(t, target, NewImporter(source, target, crypto), data)
	assertMigratedMarketPriceQuantum(t, target, NewImporter(source, target, crypto), data)
	var secret []byte
	if err = target.QueryRow(ctx, `SELECT c.secret FROM v3_catalog.channel_credentials c JOIN v3_channelmarket.groups g ON g.channel_id=c.channel_id WHERE g.public_channel_id='legacy-public-201'`).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	plain, err := crypto.Decrypt(secret)
	if err != nil || string(plain) != "fixture-upstream-secret" {
		t.Fatal("legacy source credential was not re-encrypted for v3")
	}
}
