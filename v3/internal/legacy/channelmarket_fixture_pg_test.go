//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/migrations"
)

// This fixture uses actual typed source tables. Parent migration tests can use
// it after seeding source users 7 and 8 and gateway channel 13.
func seedChannelMarketFixture(t *testing.T, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE SCHEMA marketplace`); err != nil {
		t.Fatal(err)
	}
	stamp := "2026-09-30T00:00:00Z"
	row := func(raw string) map[string]any {
		var value map[string]any
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	fixtures := map[string]map[string]any{
		"channels":                   row(`{"id":"legacy-public-201","owner_user_id":7,"provider_type":"openai","internal_channel_id":13,"declared_models":"[\"chat-model\"]","model_prices":"{}","max_concurrency":4,"user_max_concurrency":2,"qps":1.5,"multiplier_card_supported":true,"multiplier_card_user_enabled":false,"approved_source_label":"OpenAI","status":"active","model_verification_results":"{}","gpt56_mapping_results":"{}","transport_capabilities":"{}"}`),
		"groups":                     row(`{"id":"legacy-group-201","channel_id":"legacy-public-201","owner_user_id":7,"public_slug":"fixture-channel","system_display_name":"Fixture Channel","internal_group_name":"fixture_market_internal","owner_display_name":"Alice","source_type":"marketplace_user","credit_pool_policy":"marketplace_subscription_and_universal","routing_version":2,"multiplier":0.075,"visibility":"private","lifecycle_status":"active","verification_status":"passed"}`),
		"group_invites":              row(`{"id":91,"group_id":"legacy-group-201","created_by":7,"token_hash":"` + strings.Repeat("ab", 32) + `"}`),
		"group_access":               row(`{"id":92,"group_id":"legacy-group-201","user_id":8,"granted_by_invite":91}`),
		"channel_user_blocks":        row(`{"id":93,"channel_id":"legacy-public-201","user_id":8}`),
		"user_multipliers":           row(`{"id":94,"channel_id":"legacy-public-201","user_id":8,"multiplier":0.08}`),
		"time_range_multipliers":     row(`{"id":"window-201","channel_id":"legacy-public-201","start_timestamp":1790726400,"end_timestamp":1790812800,"multiplier":0.07,"label":"Night"}`),
		"multiplier_notices":         row(`{"id":95,"channel_id":"legacy-public-201","user_id":8,"previous_multiplier":0.075,"multiplier":0.08,"cleared":false,"source":"manual"}`),
		"bargain_requests":           row(`{"id":"bargain-201","group_id":"legacy-group-201","user_id":8,"proposed_multiplier":0.08,"current_multiplier":0.075,"status":"accepted","reason":"repeat consumer","admin_note":"approved"}`),
		"route_pools":                row(`{"id":"pool-201","owner_user_id":8,"name":"My pool","strategy":"priority","max_attempts":3,"failure_cooldown_seconds":30,"max_multiplier":0.5,"auto_build_enabled":true,"auto_build_models":"[\"chat-model\"]","auto_build_interval":60}`),
		"route_pool_members":         row(`{"id":1,"pool_id":"pool-201","group_id":"legacy-group-201","priority":1}`),
		"auto_route_pool_configs":    row(`{"owner_user_id":8,"strategy":"weighted","max_attempts":2,"failure_cooldown_seconds":20,"max_multiplier":1,"multiplier_weight":35,"success_weight":25,"cache_weight":15,"ttft_weight":25}`),
		"auto_route_pool_members":    row(`{"id":1,"owner_user_id":8,"group_id":"official:default","priority":2}`),
		"settlements":                row(`{"id":"settlement-201","request_id":"request-201","group_id":"legacy-group-201","owner_user_id":7,"consumer_user_id":8,"billing_source":"wallet","consumer_amount":500,"settlement_gross_amount":100,"platform_commission":5,"transaction_fee":0,"owner_net_amount":95,"reclaimed_amount":0,"multiplier":0.075,"subscription_multiplier":0.75,"status":"pending"}`),
		"income_reclaims":            row(`{"id":"reclaim-201","fingerprint":"` + strings.Repeat("cd", 32) + `","filter":"{\"OwnerUserIDs\":[7],\"MaxAmount\":10}","status":"completed","count":1,"amount":10,"owner_amounts":"{\"7\":10}","batch_number":1,"error_message":""}`),
		"verification_runs":          row(`{"id":"verify-201","channel_id":"legacy-public-201","status":"passed","stage":"model","detector_name":"test","detector_version":"1","ruleset_version":"1","evidence_hash":"sha256-evidence","summary":"passed"}`),
		"gpt56_mapping_runs":         row(`{"id":"mapping-201","channel_id":"legacy-public-201","parent_run_id":"","level":"full","trigger":"manual","status":"passed","results":"{}"}`),
		"ranking_snapshots":          row(`{"id":"ranking-201","group_id":"legacy-group-201","window_hours":24,"ranking_version":"v2","rank":1,"score":90,"raw_success_rate":0.98,"wilson_success_rate":0.95,"avg_consumer_amount":7,"avg_consumer_amount_by_model":"{\"chat-model\":7}","request_count":100,"independent_consumers":10,"observing":false}`),
		"multiplier_trend_snapshots": row(`{"id":96,"group_id":"legacy-group-201","channel_id":"legacy-public-201","source_label":"OpenAI","models":"[\"chat-model\"]","multiplier":0.075,"reliable":true,"request_count":100,"wilson_success_rate":0.95}`),
		"channel_feedback":           row(`{"id":97,"channel_id":"legacy-public-201","user_id":8,"status":"available"}`),
		"pelican_artifacts":          row(`{"group_id":"legacy-group-201","channel_id":"legacy-public-201","model":"chat-model","svg":"<svg></svg>","trigger":"manual","trigger_user_id":8,"request_id":"pelican-201","duration_ms":123}`),
	}
	fixtures["channels"]["model_prices"] = `{"chat-model":{"billing_mode":"per_call","price_per_call":0.000003},"token-model":{"input_price_per_million":1,"output_price_per_million":2.0000000000000000000000001,"cache_read_price_per_million":0.1234567890123456789,"cache_write_price_per_million":9007199254.740993}}`
	sources := map[string]string{}
	for table, values := range fixtures {
		for _, key := range []string{"created_at", "updated_at"} {
			values[key] = stamp
		}
		switch table {
		case "settlements":
			values["available_at"] = stamp
		case "gpt56_mapping_runs":
			values["started_at"] = stamp
		case "ranking_snapshots":
			values["calculated_at"] = stamp
		case "multiplier_trend_snapshots":
			values["bucket_started_at"] = stamp
			values["captured_at"] = stamp
		case "pelican_artifacts":
			values["generated_at"] = stamp
		}
		columns := make([]string, 0, len(values))
		for key := range values {
			columns = append(columns, key)
		}
		sort.Strings(columns)
		definition := make([]string, 0, len(columns))
		for _, key := range columns {
			kind := "text"
			switch v := values[key].(type) {
			case bool:
				kind = "boolean"
			case json.Number:
				kind = "numeric"
				if !strings.Contains(string(v), ".") {
					kind = "bigint"
				}
			}
			definition = append(definition, pgx.Identifier{key}.Sanitize()+" "+kind)
		}
		payload, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		name := pgx.Identifier{"marketplace", table}.Sanitize()
		if _, err = pool.Exec(ctx, `CREATE TABLE `+name+` AS SELECT * FROM jsonb_to_record($1::jsonb) AS r(`+strings.Join(definition, ",")+`)`, payload); err != nil {
			t.Fatal(err)
		}
		sources["marketplace_"+table] = name
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE public.community_channel_ratings(id bigint,channel_id text,user_id bigint,stars integer,created_at timestamptz,updated_at timestamptz);INSERT INTO public.community_channel_ratings VALUES(98,'legacy-public-201',8,5,'2026-09-30','2026-09-30')`); err != nil {
		t.Fatal(err)
	}
	sources["community_channel_ratings"] = `public.community_channel_ratings`
	var canonical bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('billing.accounts') IS NOT NULL AND to_regclass('billing.balance_snapshots') IS NOT NULL`).Scan(&canonical); err != nil {
		t.Fatal(err)
	}
	if canonical {
		if _, err := pool.Exec(ctx, `INSERT INTO billing.accounts(account_id,owner_type,owner_id,account_type,quota_unit) VALUES('fixture-market-pending-7','user',7,'marketplace_owner_pending','quota'),('fixture-market-platform-1','system',1,'marketplace_platform_revenue','quota');INSERT INTO billing.balance_snapshots(account_id,available_balance,reserved_balance) VALUES('fixture-market-pending-7',95,0),('fixture-market-platform-1',5,0)`); err != nil {
			t.Fatal(err)
		}
	}
	return sources
}

func marketMigrationDBs(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool, *catalog.AESGCM) {
	t.Helper()
	dsn := os.Getenv("V3_MIGRATION_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_MIGRATION_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	makeDB := func(kind string) *pgxpool.Pool {
		name := fmt.Sprintf("migration_cm_%s_%d", kind, time.Now().UnixNano())
		if _, err := admin.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{name}.Sanitize()); err != nil {
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
		t.Cleanup(func() {
			pool.Close()
			if _, err := admin.Exec(context.Background(), `DROP DATABASE `+pgx.Identifier{name}.Sanitize()); err != nil {
				t.Error(err)
			}
		})
		return pool
	}
	source, target := makeDB("source"), makeDB("target")
	names, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		sql, e := migrations.Read(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, err = target.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	crypto, err := catalog.NewAESGCM(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return source, target, crypto
}
