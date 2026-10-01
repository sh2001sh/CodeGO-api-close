//go:build pgintegration

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/migrations"
	"github.com/sh2001sh/new-api/v3/pkg/pg"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

type contractFixture struct {
	deps           *boot.Deps
	counts         *contractCounts
	handler        http.Handler
	key            string
	wallet, budget int64
	upstreamCalls  atomic.Int64
}

func newContractFixture(t *testing.T) *contractFixture {
	t.Helper()
	dsn, address := os.Getenv("V3_TEST_PG_DSN"), os.Getenv("V3_TEST_REDIS_ADDR")
	if dsn == "" || address == "" {
		t.Skip("requires isolated V3_TEST_PG_DSN and V3_TEST_REDIS_ADDR")
	}
	t.Setenv("V3_BILLING_WAL_DIR", "off")
	t.Setenv("V3_TRUSTED_PROXY_CIDRS", "")
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	random := make([]byte, 8)
	if _, err = rand.Read(random); err != nil {
		t.Fatal(err)
	}
	database := "v3_request_contract_" + hex.EncodeToString(random)
	quoted := pgx.Identifier{database}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE "+quoted); err != nil {
			t.Errorf("clean isolated test database: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	f := &contractFixture{counts: &contractCounts{}}
	config.ConnConfig.Database, config.ConnConfig.Tracer = database, f.counts
	config.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	names, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		sql, err := migrations.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, sql); err != nil {
			t.Fatalf("migration %s: %v", name, err)
		}
	}
	// DB15 separates this production-wired fixture from suites using DB0.
	// Refuse existing data; cleanup deletes exact keys in the initially empty DB.
	rdb, err := redisx.Connect(redisx.Config{Addr: address, DB: 15})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	size, err := rdb.DBSize(ctx).Result()
	if err != nil || size != 0 {
		t.Fatalf("test Redis DB15 must initially be empty: size=%d err=%v", size, err)
	}
	t.Cleanup(func() {
		keys, err := rdb.Keys(ctx, "v3:*").Result()
		if err != nil {
			t.Errorf("list owned test keys: %v", err)
		} else if len(keys) > 0 {
			if err = rdb.Del(ctx, keys...).Err(); err != nil {
				t.Errorf("delete owned test keys: %v", err)
			}
		}
	})
	rdb.AddHook(f.counts)
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	crypto, err := catalog.NewAESGCM(secret)
	if err != nil {
		t.Fatal(err)
	}
	f.deps = &boot.Deps{PG: &pg.Pool{Pool: pool}, Redis: rdb, Crypto: crypto}
	upstreams := []*httptest.Server{
		httptest.NewServer(f.upstream(true)), httptest.NewServer(f.upstream(false)),
	}
	for _, upstream := range upstreams {
		t.Cleanup(upstream.Close)
	}
	f.seed(t, upstreams)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err = catalog.NewPublisher(pool, rdb, crypto, crypto, log).PublishNow(ctx); err != nil {
		t.Fatal(err)
	}
	background, cancel := context.WithCancel(context.WithValue(ctx, contractContextKey{}, contractBackground))
	handler, closeFn, err := prodHandler(background, f.deps, log)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	f.handler = handler
	t.Cleanup(func() { cancel(); closeFn() })
	return f
}

func (f *contractFixture) seed(t *testing.T, upstreams []*httptest.Server) {
	t.Helper()
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		if _, err := f.deps.PG.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO v3_identity.users(id,username,max_concurrency,requests_per_minute) VALUES(1,'contract-user',1,100)`)
	key, hash, prefix, err := identity.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	f.key = key
	sealed, err := f.deps.Crypto.Encrypt([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO v3_identity.api_keys(id,user_id,key_hash,key_prefix,key_ciphertext,budget_limited,allowed_models,allowed_cidrs) VALUES(1,1,$1,$2,$3,true,'{contract-model}','{127.0.0.0/8}')`, hash[:], prefix, sealed)
	for _, account := range []struct {
		owner, kind string
		id          *int64
	}{{"user", "wallet", &f.wallet}, {"api_key", "key_budget", &f.budget}} {
		if err = f.deps.PG.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind,balance) VALUES($1,1,$2,1000000000) RETURNING id`, account.owner, account.kind).Scan(account.id); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO v3_catalog.groups(name,multiplier) VALUES('default',1)`)
	exec(`INSERT INTO v3_catalog.model_prices(model,input_per_mtok,output_per_mtok) VALUES('contract-model',1000000,2000000)`)
	credential, err := f.deps.Crypto.Encrypt([]byte("upstream-test-key"))
	if err != nil {
		t.Fatal(err)
	}
	for i, upstream := range upstreams {
		id := i + 1
		exec(`INSERT INTO v3_catalog.channels(id,name,provider,base_url,priority,max_concurrency,max_user_concurrency) VALUES($1,$2,'openai',$3,$4,1,1)`, id, fmt.Sprint("contract-", id), upstream.URL, 10-i)
		exec(`INSERT INTO v3_catalog.channel_credentials(channel_id,secret,max_concurrency) VALUES($1,$2,1)`, id, credential)
		exec(`INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES($1,'contract-model')`, id)
		exec(`INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES($1,'default')`, id)
	}
}

func (f *contractFixture) upstream(first bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.upstreamCalls.Add(1)
		var body struct {
			Stream   bool `json:"stream"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid fixture request", 400)
			return
		}
		if first && len(body.Messages) > 0 && body.Messages[0].Content == "retry" {
			http.Error(w, "retry fixture", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"id\":\"contract\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"contract\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\ndata: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, `{"id":"contract","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	})
}

func contractBalanceKey(id int64) string { return fmt.Sprintf("%s{%d}", redisx.KeyBalancePrefix, id) }

func (f *contractFixture) balances(t *testing.T) [2]string {
	t.Helper()
	var out [2]string
	for i, id := range []int64{f.wallet, f.budget} {
		value, err := f.deps.Redis.HGet(context.Background(), contractBalanceKey(id), "balance").Result()
		if err != nil {
			t.Fatal(err)
		}
		out[i] = value
	}
	return out
}

func contractFutureScore() int64 { return time.Now().Add(time.Hour).UnixMilli() }
