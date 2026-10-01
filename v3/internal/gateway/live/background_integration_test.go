package live

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestBackgroundRealRedisRestartChargesOnlyAtTerminal(t *testing.T) {
	addr := os.Getenv("CODEGO_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("CODEGO_TEST_REDIS_ADDR is required")
	}
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = client.Close() })
	prefix := t.Name() + ":" + requestID()
	key := bytes.Repeat([]byte{9}, 32)
	repo, err := NewRedisBackgroundRepository(client, prefix, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		keys, err := client.Keys(context.Background(), repo.prefix+"*").Result()
		if err == nil && len(keys) > 0 {
			_ = client.Del(context.Background(), keys...).Err()
		}
	})
	var posts, gets atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			posts.Add(1)
			_, _ = fmt.Fprint(w, `{"id":"resp_up","object":"response","status":"queued","output":[]}`)
		case http.MethodGet:
			gets.Add(1)
			_, _ = fmt.Fprint(w, backgroundCompletedSnapshot)
		}
	}))
	defer up.Close()
	h, _, _, _, billing := backgroundJobsFixture(t, up.URL, "openai")
	h.cfg.BackgroundJobs = repo
	mux := http.NewServeMux()
	h.registerBackground(mux)
	id := createBackgroundForTest(t, h.Wrap(mux), "/responses", `{"model":"gpt-test","input":"hello","background":true}`)
	if err := h.Reconcile(ctx, 8); err != nil {
		t.Fatal(err)
	}
	queued, err := repo.GetOwned(ctx, id, 1, 11)
	if err != nil || queued.Billed || queued.UpstreamID != "resp_up" {
		t.Fatalf("queued job=%+v %v", queued, err)
	}
	if posts.Load() != 1 || gets.Load() != 0 {
		t.Fatalf("native acceptance counters post=%d get=%d", posts.Load(), gets.Load())
	}
	// Expire the old process's lease before the replacement worker takes over.
	if _, err := repo.Claim(ctx, id, queued.LeaseID, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Millisecond)
	restartedRepo, err := NewRedisBackgroundRepository(client, prefix, key)
	if err != nil {
		t.Fatal(err)
	}
	restartedCfg := h.cfg
	restartedCfg.BackgroundJobs = restartedRepo
	restarted, err := New(restartedCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Reconcile(ctx, 8); err != nil {
		t.Fatal(err)
	}
	finished, err := restartedRepo.GetOwned(ctx, id, 1, 11)
	if err != nil || !finished.Billed || finished.Status != "completed" {
		t.Fatalf("finished job=%+v %v", finished, err)
	}
	if err := restarted.Reconcile(ctx, 8); err != nil {
		t.Fatal(err)
	}
	out, charges, _ := billing.result(id)
	if charges != 1 || !out.Charge || out.Usage.PromptTokens != 5 || out.Usage.CompletionTokens != 3 || posts.Load() != 1 || gets.Load() != 1 {
		t.Fatalf("charge=%d outcome=%+v posts=%d gets=%d", charges, out, posts.Load(), gets.Load())
	}
}
