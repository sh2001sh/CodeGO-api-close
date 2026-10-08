//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestHistoryPostgresLocalMeanTimeRoundTrip(t *testing.T) {
	dsn := os.Getenv("V3_MIGRATION_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_MIGRATION_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `CREATE TEMP TABLE historical_time_roundtrip (zone text, at timestamptz)`); err != nil {
		t.Fatal(err)
	}
	for _, zone := range []string{"Asia/Shanghai", "Europe/Berlin"} {
		if _, err = tx.Exec(ctx, `SELECT set_config('TimeZone',$1,true)`, zone); err != nil {
			t.Fatal(err)
		}
		for _, instant := range []string{"0001-01-01T00:00:00Z", "1900-01-01T00:00:00.123456Z"} {
			var raw json.RawMessage
			var original time.Time
			err = tx.QueryRow(ctx, `SELECT jsonb_build_object('request_id','old-request','status','in_flight',
			'completed_at',$1::timestamptz),$1::timestamptz`, instant).Scan(&raw, &original)
			if err != nil {
				t.Fatal(err)
			}
			audit, err := decodeHistoryRequestAudit(raw)
			if err != nil || !audit.CompletedAt.Equal(original) || audit.Status != "historical_unknown" || audit.CountedInSuccessRate {
				t.Fatalf("historical audit changed: %+v, %v; source instant %s", audit, err, original)
			}
			// The import and typed checker must agree with the original SQL instant,
			// including an unset completed_at retained by historical_unknown.
			encoded, err := json.Marshal(audit.completedDate())
			if err != nil {
				t.Fatal(err)
			}
			fields, err := historyProjectionFields(map[string]json.RawMessage{"completed_at": encoded})
			if err != nil || !fields["completed_at"].(time.Time).Equal(original) {
				t.Fatalf("check projection changed instant: %v, %v", fields, err)
			}
			if _, err = tx.Exec(ctx, `INSERT INTO historical_time_roundtrip VALUES ($1,$2)`, zone, audit.completedDate()); err != nil {
				t.Fatal(err)
			}
			var changed int
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM historical_time_roundtrip WHERE at=$1::timestamptz AND zone=$2`, instant, zone).Scan(&changed); err != nil || changed != 1 {
				t.Fatalf("stored timestamp changed: matching rows %d, %v", changed, err)
			}
		}
	}
	var raw json.RawMessage
	if err = tx.QueryRow(ctx, `SELECT jsonb_build_object('request_id','invalid-request','status','succeeded',
		'completed_at','infinity'::timestamptz)`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if _, err = decodeHistoryRequestAudit(raw); err == nil {
		t.Fatal("invalid infinite source timestamp accepted")
	}
}
