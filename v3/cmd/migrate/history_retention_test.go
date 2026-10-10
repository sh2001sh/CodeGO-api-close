package main

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestMigrationHistoryCutoffIsExplicitAndValidatedBeforeConnections(t *testing.T) {
	t.Setenv("V3_MIGRATION_HISTORY_CUTOFF", "")
	cutoff, err := migrationHistoryCutoff()
	if err != nil || !cutoff.IsZero() {
		t.Fatalf("default=%v %v", cutoff, err)
	}
	t.Setenv("V3_MIGRATION_HISTORY_CUTOFF", "2026-09-10T12:00:00+08:00")
	cutoff, err = migrationHistoryCutoff()
	if err != nil || cutoff.Format(time.RFC3339) != "2026-09-10T04:00:00Z" {
		t.Fatalf("cutoff=%v %v", cutoff, err)
	}
	for _, value := range []string{"garbage", "1969-12-31T23:59:59Z", "9999-01-01T00:00:00Z", "2026-09-10T04:00:00.1Z"} {
		t.Setenv("V3_MIGRATION_HISTORY_CUTOFF", value)
		for _, command := range []string{"import", "check", "online-prepare"} {
			var out bytes.Buffer
			if err := run(context.Background(), []string{command}, &out); err == nil {
				t.Fatalf("%s accepted %s", command, value)
			}
			if out.Len() != 0 {
				t.Fatal("invalid cutoff should not start a database operation")
			}
		}
	}
}
