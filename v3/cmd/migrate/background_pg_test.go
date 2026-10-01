//go:build pgintegration

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/legacy"
)

func backgroundCLISource(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("V3_MIGRATION_TEST_PG_DSN")
	if dsn == "" || os.Getenv("V3_MIGRATION_TEST_REDIS_ADDR") == "" {
		t.Skip("disposable V3_MIGRATION_TEST_PG_DSN and V3_MIGRATION_TEST_REDIS_ADDR are required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(ctx) })
	suffix := make([]byte, 8)
	if _, err = rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "migration_bg_cli_" + hex.EncodeToString(suffix)
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `CREATE TABLE users(id bigint); INSERT INTO users VALUES(7);
		CREATE TABLE tokens(id bigint,user_id bigint); INSERT INTO tokens VALUES(11,7);
		CREATE TABLE channels(id bigint); INSERT INTO channels VALUES(13);
		CREATE SCHEMA gateway;
		CREATE TABLE gateway.responses_background_jobs(id text,user_id bigint,token_id bigint,model text,status text,stream bool,native_background bool,
		channel_id bigint,last_sequence bigint,created_at timestamptz,updated_at timestamptz,final_response_ciphertext text);
		INSERT INTO gateway.responses_background_jobs VALUES('resp_bg_cli_terminal',7,11,'source-model','completed',false,false,13,-1,
		'2026-09-20T01:02:03.123456Z','2026-09-20T01:03:03.123456Z','{"id":"resp_bg_cli_terminal","status":"completed","output":[]}');`)
	if err != nil {
		t.Fatal(err)
	}
	// A server-side read-only default additionally protects this CLI fixture.
	config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	connectionURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	connectionURL.Path = "/" + name
	query := connectionURL.Query()
	query.Set("default_transaction_read_only", "on")
	connectionURL.RawQuery = query.Encode()
	t.Setenv("V3_SOURCE_PG_DSN", connectionURL.String())
	return reader
}

func TestBackgroundCLIRequiresOfflineAndUsesSourceWithoutTargetPostgres(t *testing.T) {
	backgroundCLISource(t)
	t.Setenv("V3_PG_DSN", "")
	t.Setenv("V3_SECRET_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	t.Setenv("V3_REDIS_ADDR", os.Getenv("V3_MIGRATION_TEST_REDIS_ADDR"))
	t.Setenv("V3_REDIS_PASSWORD", "")
	ctx := context.Background()
	var output bytes.Buffer
	if err := run(ctx, []string{"background"}, &output); err != nil {
		t.Fatal(err)
	}
	var report legacy.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || report.Applied || report.Counts["responses_background_jobs"] != 1 {
		t.Fatal("CLI source-only preview failed")
	}
	output.Reset()
	if err := run(ctx, []string{"background", "-apply"}, &output); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatal("CLI accepted application without offline assertion")
	}
	for range 2 {
		output.Reset()
		if err := run(ctx, []string{"background", "-apply", "-offline"}, &output); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(output.Bytes(), &report); err != nil || !report.Applied || report.Counts["check:responses_background_jobs"] != 1 {
			t.Fatal("CLI asset application failed")
		}
	}
	repository, closeClient, err := legacy.OpenBackgroundMigrationRepository(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient()
	job, err := repository.GetOwned(ctx, "resp_bg_cli_terminal", 7, 11)
	if err != nil || job.Status != "completed" || !job.Billed || job.UpdatedAt.Nanosecond() != 123456000 {
		t.Fatal("CLI published unusable history")
	}
}
