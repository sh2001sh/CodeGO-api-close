//go:build pgintegration

package catalog

import (
	"context"
	"testing"
)

func TestCompileConcurrencyLimits(t *testing.T) {
	pool := testPool(t)
	enc, dec := testDecrypter(t)
	seedCatalog(t, pool, enc)
	mustExec(t, pool, `UPDATE v3_catalog.channels SET max_concurrency = 10, max_user_concurrency = 2 WHERE id = 1`)
	mustExec(t, pool, `UPDATE v3_catalog.channel_credentials SET max_concurrency = 3 WHERE channel_id = 1`)
	snapshot, err := Compile(context.Background(), pool, dec)
	if err != nil {
		t.Fatal(err)
	}
	channel := snapshot.Channels[1]
	if channel.MaxConcurrency != 10 || channel.MaxUserConcurrency != 2 || len(channel.Credentials) != 1 || channel.Credentials[0].MaxConcurrency != 3 {
		t.Fatalf("compiled limits: channel=%+v", channel)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE v3_catalog.channel_credentials SET max_concurrency = -1 WHERE channel_id = 1`); err == nil {
		t.Fatal("negative credential concurrency accepted")
	}
}
