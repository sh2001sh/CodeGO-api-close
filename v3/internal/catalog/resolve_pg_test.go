//go:build pgintegration

package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestDurableTargetReferenceRetainsDisabledAndFailsDeleted(t *testing.T) {
	pool := testPool(t)
	dec, enc := testDecrypter(t)
	seedCatalog(t, pool, enc)
	mustExec(t, pool, `UPDATE v3_catalog.channels SET status='disabled',proxy_url='http://proxy.example:9000',settings='{"reasoning":"high"}',param_override='{"temperature":0}',header_override='{"X-Saved":"yes"}',status_code_mapping='{"400":"503"}' WHERE id=1`)
	mustExec(t, pool, `UPDATE v3_catalog.channel_credentials SET expires_at=now()-interval '1 hour',max_concurrency=3,fingerprint='{"user_agent":"durable-ua","tls_profile":"chrome"}' WHERE id=1`)
	channel, credential, err := ResolveReference(context.Background(), pool, dec, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if channel.ProxyURL != "http://proxy.example:9000" || channel.Settings["reasoning"] != "high" || channel.HeaderOverride["X-Saved"] != "yes" || channel.StatusCodeMapping["400"] != 503 || credential.MaxConcurrency != 3 || credential.Fingerprint.UserAgent != "durable-ua" || credential.Secret != "sk-alpha" {
		t.Fatal("durable reference metadata lost")
	}
	if _, _, err := ResolveReference(context.Background(), pool, dec, 2, 1); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-channel reference: %v", err)
	}
	mustExec(t, pool, `DELETE FROM v3_catalog.channel_credentials WHERE id=1`)
	if _, _, err := ResolveReference(context.Background(), pool, dec, 1, 1); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deleted reference: %v", err)
	}
}
