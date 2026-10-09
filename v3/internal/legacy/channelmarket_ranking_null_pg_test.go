//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestChannelMarketRankingNullMetricsTypedSourceAndOnlineProjection(t *testing.T) {
	source, target, _ := marketMigrationDBs(t)
	ctx := context.Background()
	// GORM's older rows have explicit NULL in these later-added doubles.
	onlineRecoveryExec(t, source, `CREATE TABLE ranking_null_metrics (
 id text PRIMARY KEY,group_id text,window_hours integer,ranking_version text,rank integer,
 score double precision,raw_success_rate double precision,wilson_success_rate double precision,avg_ttft_ms double precision,
 attempt_ttft_p50_ms double precision,attempt_ttft_p95_ms double precision,e2e_ttft_p50_ms double precision,e2e_ttft_p95_ms double precision,
 avg_latency_ms double precision,avg_tps double precision,cache_hit_rate double precision,
 latency_sample_count bigint,avg_consumer_amount bigint,avg_consumer_amount_by_model text,
 request_count bigint,independent_consumers bigint,observing boolean,calculated_at timestamptz);
 INSERT INTO ranking_null_metrics VALUES
 ('ranking-null','g-201',24,'v2',1,90,98.5,95.25,0.12345678901234566,NULL,NULL,NULL,NULL,-3.25,8.875,0.25,0,7,'{"chat-model":7}',100,10,false,'2026-09-30'),
 ('ranking-value','g-201',168,'v2',2,-0.125,98.5,95.25,0.12345678901234566,0.12345678901234566,9007199254740991,-9.25,1e-20,-3.25,8.875,0.25,1,7,'{"chat-model":7}',100,10,false,'2026-09-30')`)
	var before string
	digestQuery := "SELECT md5(string_agg(to_jsonb(r)::text,',' ORDER BY id)) FROM ranking_null_metrics r"
	if err := source.QueryRow(ctx, digestQuery).Scan(&before); err != nil {
		t.Fatal(err)
	}
	read, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(ctx) }()
	inputs, err := onlineReadBatch(ctx, read, onlineSpec{name: "market.ranking_snapshots", source: "ranking_null_metrics", keys: []string{"id"}}, nil)
	if err != nil || len(inputs) != 2 {
		t.Fatalf("typed online source batch: rows=%d err=%v", len(inputs), err)
	}
	data := &channelMarketData{groups: map[string]cmRow{"g-201": {"id": json.RawMessage(`"g-201"`)}}}
	p := &onlineProjector{market: data}
	var projected []map[string]any
	for _, input := range inputs {
		var row cmRow
		if err := json.Unmarshal(input.raw, &row); err != nil {
			t.Fatal(err)
		}
		if row.text("id") == "ranking-null" {
			for _, key := range []string{"attempt_ttft_p50_ms", "attempt_ttft_p95_ms", "e2e_ttft_p50_ms", "e2e_ttft_p95_ms"} {
				if string(row[key]) != "null" {
					t.Fatalf("typed source did not emit explicit JSON null for %s", key)
				}
			}
		}
		// Offline streaming import and online copy use this common projection.
		offline, err := data.projectMarketHistory("ranking_snapshots", row)
		if err != nil {
			t.Fatal(err)
		}
		online, err := p.projectOnlineMarket("ranking_snapshots", input.raw)
		if err != nil || len(online) != 1 || !reflect.DeepEqual(offline.values, online[0].Values) {
			t.Fatalf("offline/online ranking projections differ: %v", err)
		}
		projected = append(projected, online[0].Values)
	}
	write, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = write.Rollback(ctx) }()
	if _, err := write.Exec(ctx, `CREATE SCHEMA v3_ranking_null_test;
 CREATE TABLE v3_ranking_null_test.ranking_snapshots (LIKE v3_channelmarket.ranking_snapshots INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES)`); err != nil {
		t.Fatal(err)
	}
	if err := insertExactBulk(ctx, write, "v3_ranking_null_test", "ranking_snapshots", []string{"id"}, projected); err != nil {
		t.Fatal(err)
	}
	for _, input := range inputs {
		var row cmRow
		if err := json.Unmarshal(input.raw, &row); err != nil {
			t.Fatal(err)
		}
		id := row.text("id")
		for _, field := range []string{"score", "raw_success_rate", "wilson_success_rate", "avg_ttft_ms", "attempt_ttft_p50_ms", "attempt_ttft_p95_ms", "e2e_ttft_p50_ms", "e2e_ttft_p95_ms", "avg_latency_ms", "avg_tps", "cache_hit_rate"} {
			column := pgx.Identifier{field}.Sanitize()
			var want, got float64
			if err := read.QueryRow(ctx, "SELECT COALESCE("+column+",0) FROM ranking_null_metrics WHERE id=$1", id).Scan(&want); err != nil {
				t.Fatal(err)
			}
			if err := write.QueryRow(ctx, "SELECT "+column+" FROM v3_ranking_null_test.ranking_snapshots WHERE id=$1", id).Scan(&got); err != nil || got != want {
				t.Fatalf("metric %s.%s got=%v want=%v err=%v", id, field, got, want, err)
			}
		}
	}
	var moneyUnchanged bool
	if err := write.QueryRow(ctx, `SELECT bool_and(avg_consumer_micro=14 AND avg_consumer_micro_by_model='{"chat-model":14}'::jsonb) FROM v3_ranking_null_test.ranking_snapshots`).Scan(&moneyUnchanged); err != nil || !moneyUnchanged {
		t.Fatalf("ranking metric compatibility changed exact amounts: %t %v", moneyUnchanged, err)
	}
	matches, err := checkExactBulk(ctx, write, "v3_ranking_null_test", "ranking_snapshots", []string{"id"}, projected)
	if err != nil || !reflect.DeepEqual(matches, []bool{true, true}) {
		t.Fatalf("independent typed check failed: %v %v", matches, err)
	}
	if _, err := write.Exec(ctx, "UPDATE v3_ranking_null_test.ranking_snapshots SET attempt_ttft_p50_ms=1 WHERE id='ranking-null'"); err != nil {
		t.Fatal(err)
	}
	matches, err = checkExactBulk(ctx, write, "v3_ranking_null_test", "ranking_snapshots", []string{"id"}, projected)
	if err != nil || !reflect.DeepEqual(matches, []bool{false, true}) {
		t.Fatalf("independent check accepted a changed NULL-derived metric: %v %v", matches, err)
	}
	var after string
	if err := source.QueryRow(ctx, digestQuery).Scan(&after); err != nil || before != after {
		t.Fatalf("projection modified source ranking rows: %v", err)
	}
	t.Log("typed PG NULL metrics become zero; non-NULL doubles, negatives, money and source rows are preserved; typed check detects tampering")
}
