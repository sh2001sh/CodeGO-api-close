//go:build pgintegration

package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/pkg/pg"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

type summaryWorkerFixture struct {
	deps    *boot.Deps
	billing *billing.Settler
	native  *nativeJobs
	target  gateway.Target
}

func newSummaryWorkerFixture(t *testing.T) *summaryWorkerFixture {
	t.Helper()
	address := os.Getenv("V3_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("requires disposable V3_TEST_REDIS_ADDR")
	}
	pool := jobsTestPool(t)
	ctx := context.Background()
	rdb, err := redisx.Connect(redisx.Config{Addr: address, DB: 14})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := rdb.DBSize(ctx).Result(); err != nil || n != 0 {
		t.Fatalf("isolated Redis DB14 must be empty: %d %v", n, err)
	}
	t.Cleanup(func() {
		keys, err := rdb.Keys(ctx, "*").Result()
		if err != nil {
			t.Error(err)
		} else if len(keys) > 0 {
			if err := rdb.Del(ctx, keys...).Err(); err != nil {
				t.Error(err)
			}
		}
		if err := rdb.Close(); err != nil {
			t.Error(err)
		}
	})
	crypto, err := catalog.NewAESGCM(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	f := &summaryWorkerFixture{deps: &boot.Deps{PG: &pg.Pool{Pool: pool}, Redis: rdb, Crypto: crypto}}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/responses" {
			_, _ = io.WriteString(w, `{"id":"provider-response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"private-answer"}]}],"usage":{"input_tokens":10,"output_tokens":5,"input_tokens_details":{"cached_tokens":5}}}`)
		} else if strings.Contains(r.URL.Path, "failed") {
			_, _ = io.WriteString(w, `{"id":"video-failed","status":"failed","error":{"message":"private-failure"}}`)
		} else {
			_, _ = io.WriteString(w, `{"id":"video-success","status":"completed","seconds":"1","url":"https://fixture.invalid/result"}`)
		}
	}))
	t.Cleanup(upstream.Close)
	exec := func(q string, args ...any) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO v3_identity.users(id,username) VALUES(1,'summary-worker'); INSERT INTO v3_billing.accounts(owner_type,owner_id,kind,balance) VALUES('user',1,'wallet',1000000); INSERT INTO v3_catalog.groups(name) VALUES('default'); INSERT INTO v3_catalog.model_prices(model,mode,per_request) VALUES('fixture-model','per_request',25)`)
	key, hash, prefix, err := identity.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	keyCipher, err := crypto.Encrypt([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO v3_identity.api_keys(id,user_id,key_hash,key_prefix,key_ciphertext) VALUES(1,1,$1,$2,$3)`, hash[:], prefix, keyCipher)
	secret, err := crypto.Encrypt([]byte("private-credential"))
	if err != nil {
		t.Fatal(err)
	}
	for i, provider := range []string{"openai", "openai_video"} {
		id := int64(i + 1)
		exec(`INSERT INTO v3_catalog.channels(id,name,provider,base_url) VALUES($1,$2,$3,$4)`, id, provider, provider, upstream.URL)
		exec(`INSERT INTO v3_catalog.channel_credentials(id,channel_id,secret) OVERRIDING SYSTEM VALUE VALUES($1,$1,$2)`, id, secret)
		exec(`INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES($1,'fixture-model')`, id)
		exec(`INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES($1,'default')`, id)
	}
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	store := catalog.NewStore(pool, rdb, crypto, log)
	if err := catalog.NewPublisher(pool, rdb, crypto, crypto, log).PublishNow(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Load(ctx); err != nil {
		t.Fatal(err)
	}
	accounts := ledger.NewAccounts(pool)
	f.billing, err = billing.New(rdb, store.Current, accounts, accounts, billing.Config{DisableOutageAdmission: true}, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.billing.Close)
	t.Setenv("V3_FILES_DIR", t.TempDir())
	f.native, err = newNativeJobs(f.deps, f.billing, store.Current, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.native.close)
	f.target, err = f.deps.ResolveTarget(ctx, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.target.Group = "default"
	f.target.MultiplierPPM = 1_000_000
	return f
}
