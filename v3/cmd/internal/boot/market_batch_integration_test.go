//go:build pgintegration

package boot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/migrations"
)

func marketBatchPG(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	rows, err := pool.Query(ctx, `SELECT nspname FROM pg_namespace WHERE left(nspname,3)='v3_'`)
	if err != nil {
		t.Fatal(err)
	}
	var schemas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		schemas = append(schemas, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range schemas {
		if _, err := pool.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{name}.Sanitize()+" CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	files, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		source, err := migrations.Read(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, source); err != nil {
			t.Fatalf("apply %s: %v", file, err)
		}
	}
	for id := 1; id <= 3; id++ {
		if _, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES($1,$2)`, id, fmt.Sprintf("batch-user-%d", id)); err != nil {
			t.Fatal(err)
		}
	}
	return pool
}

// The HTTP fixture enforces real issued-key policy and posts real ledger money;
// production composition uses the same normal gateway endpoint and signature.
func TestMarketBatchFactoryChargesOnceWaitsForReceiptAndRevokesKey(t *testing.T) {
	pool := marketBatchPG(t)
	ctx := context.Background()
	secret := bytes.Repeat([]byte{9}, 32)
	crypto, _ := catalog.NewAESGCM(secret)
	poster := ledger.NewPoster(pool)
	keys, err := NewMarketBatchIdentity(pool, base64.StdEncoding.EncodeToString(secret), nil)
	if err != nil {
		t.Fatal(err)
	}
	service := channelmarket.New(pool, crypto, poster, channelmarket.Config{Probe: func(_ context.Context, p channelmarket.ProbeRequest) (channelmarket.ModelTest, error) {
		return channelmarket.ModelTest{Model: p.Model, Status: "passed"}, nil
	}}, nil)
	channel, err := service.Create(ctx, 1, channelmarket.CreateRequest{Provider: "openai", BaseURL: "https://example.com", APIKey: "fixture-secret", Models: []string{"fixture-model"}, Visibility: "public", Multiplier: json.Number("0.1")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.QueueVerification(ctx, channelmarket.Actor{UserID: 1}, channel.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ProcessVerification(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := service.Transition(ctx, channelmarket.Actor{UserID: 3, Admin: true}, channel.InternalChannelID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	var internalGroup string
	if err := pool.QueryRow(ctx, `SELECT internal_group_name FROM v3_channelmarket.groups WHERE id=$1`, channel.GroupID).Scan(&internalGroup); err != nil {
		t.Fatal(err)
	}
	var wallet int64
	if err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind,balance) VALUES('user',2,'wallet',1000) RETURNING id`).Scan(&wallet); err != nil {
		t.Fatal(err)
	}
	var mode, calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		id := MarketBatchRequestID(r, crypto.DeriveKey("market-batch"))
		if id == "" {
			t.Error("normal gateway did not receive valid durable signature")
			w.WriteHeader(403)
			return
		}
		var principal gateway.Principal
		var expires time.Time
		hash := identity.HashKey(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		err := pool.QueryRow(r.Context(), `SELECT user_id,id,group_name,allowed_models,expires_at,cross_group_retry
		FROM v3_identity.api_keys WHERE key_hash=$1 AND status='active' AND deleted_at IS NULL`, hash[:]).Scan(&principal.UserID, &principal.KeyID, &principal.Group, &principal.AllowedModels, &expires, &principal.CrossGroupRetry)
		body, readErr := io.ReadAll(r.Body)
		var input struct {
			Model string `json:"model"`
		}
		if err != nil || readErr != nil || json.Unmarshal(body, &input) != nil || principal.UserID != 2 || principal.Group != internalGroup || principal.CrossGroupRetry ||
			!expires.After(time.Now()) || expires.After(time.Now().Add(4*time.Minute)) || gateway.ValidateRequestPolicy(principal, input.Model, r, nil) != nil || len(principal.AllowedModels) != 1 {
			t.Errorf("issued key policy lost: %+v %v", principal, err)
			w.WriteHeader(403)
			return
		}
		w.Header().Set("X-Request-Id", id)
		if mode.Load() == 1 {
			w.WriteHeader(429)
			return
		}
		if mode.Load() == 2 {
			_, _ = io.WriteString(w, `{"choices":[]}`)
			return
		}
		err = pgx.BeginFunc(r.Context(), pool, func(tx pgx.Tx) error {
			if _, err := poster.PostTx(r.Context(), tx, billing.Entry{AccountID: wallet, Amount: -20, Kind: "usage", OperationID: id, RequestID: id, Reason: "charged normal test request"}); err != nil {
				return err
			}
			_, err := tx.Exec(r.Context(), `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,key_id,channel_id,amount,request_id,model,terminal) VALUES(now(),$1,2,$2,$3,20,$4,'fixture-model','completed')`, wallet, principal.KeyID, channel.InternalChannelID, id)
			return err
		})
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"OK"}}]}`)
	}))
	defer server.Close()
	relay, err := NewMarketBatchRelay(pool, keys, MarketBatchConfig{GatewayBaseURL: server.URL, SigningKey: crypto.DeriveKey("market-batch")})
	if err != nil {
		t.Fatal(err)
	}
	service = channelmarket.New(pool, crypto, poster, channelmarket.Config{BatchRelay: relay}, nil)
	enqueue := func() channelmarket.BatchRelayRequest {
		t.Helper()
		job, err := service.StartBatch(ctx, 2, channelmarket.BatchRequest{GroupIDs: []string{channel.GroupID}, Model: "fixture-model"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE v3_channelmarket.batch_test_items SET status='running',started_at=now() WHERE batch_id=$1`, job.ID); err != nil {
			t.Fatal(err)
		}
		return channelmarket.BatchRelayRequest{UserID: 2, Group: internalGroup, Model: "fixture-model", RequestID: job.Items[0].RequestID}
	}
	request := enqueue()
	var successful atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := relay(ctx, request)
			if err == nil {
				successful.Add(1)
				if receipt.AmountMicro != 20 || !receipt.LogCreated || receipt.BillingSource != "wallet" {
					t.Errorf("forged/missing durable receipt: %+v", receipt)
				}
			} else if !errors.Is(err, channelmarket.ErrConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if successful.Load() != 1 || calls.Load() != 1 {
		t.Fatalf("generation replay: successes=%d calls=%d", successful.Load(), calls.Load())
	}
	for _, failureMode := range []int64{1, 2} {
		mode.Store(failureMode)
		input := enqueue()
		callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := relay(callCtx, input)
		cancel()
		if err == nil || (failureMode == 2 && !errors.Is(err, context.DeadlineExceeded)) {
			t.Fatalf("failure fabricated success mode=%d err=%v", failureMode, err)
		}
		if _, err := relay(ctx, input); !errors.Is(err, channelmarket.ErrConflict) {
			t.Fatalf("uncertain failed request regenerated: %v", err)
		}
	}
	mode.Store(0)
	job, err := service.StartBatch(ctx, 2, channelmarket.BatchRequest{GroupIDs: []string{channel.GroupID}, Model: "fixture-model"})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := service.ProcessBatchTests(ctx, 2); err != nil || count != 1 {
		t.Fatalf("actual durable worker dispatch: count=%d err=%v", count, err)
	}
	job, err = service.Batch(ctx, 2, job.ID)
	if err != nil || job.Status != "completed" || len(job.Items) != 1 || job.Items[0].Status != "passed" || job.Items[0].AmountMicro != 20 || !job.LogCreated {
		t.Fatalf("durable worker lost actual receipt: %+v %v", job, err)
	}
	var balance, activeKeys int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE id=$1`, wallet).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_identity.api_keys WHERE deleted_at IS NULL`).Scan(&activeKeys); err != nil {
		t.Fatal(err)
	}
	if balance != 960 || activeKeys != 0 || calls.Load() != 4 {
		t.Fatalf("money/key lifecycle failure: wallet=%d active_keys=%d calls=%d", balance, activeKeys, calls.Load())
	}
	blocked := enqueue()
	if err := service.SetBlock(ctx, channelmarket.Actor{UserID: 1}, channel.InternalChannelID, 2, true); err != nil {
		t.Fatal(err)
	}
	if _, err := relay(ctx, blocked); !errors.Is(err, identity.ErrForbidden) || calls.Load() != 4 {
		t.Fatalf("revoked group access dispatched upstream: %v calls=%d", err, calls.Load())
	}
}
