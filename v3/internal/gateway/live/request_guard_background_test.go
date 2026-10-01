package live

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func backgroundPathTarget(h *Handler, path string) {
	target, _ := h.cfg.Resolve(context.Background(), 7, 9)
	target.ParamOverride = map[string]any{"operations": []any{
		map[string]any{"mode": "set", "path": "metadata.source", "value": "preserved", "conditions": map[string]any{"request_path": path}},
	}}
	h.cfg.Planner = backgroundJobsPlanner{target: target}
	h.cfg.Resolve = func(context.Context, int64, int64) (gateway.Target, error) { return target, nil }
}

func TestBackgroundGuardRunsOnlyAtCreationAndRestoresOriginalPath(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses", "legacy-empty"} {
		t.Run(path, func(t *testing.T) {
			var calls, checks atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				body, _ := io.ReadAll(r.Body)
				if gjson.GetBytes(body, "metadata.source").Str != "preserved" {
					t.Errorf("restored path did not drive actual channel condition: %s", body)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, backgroundCompletedSnapshot)
			}))
			defer upstream.Close()
			h, _, wrapped, repo, billing := backgroundJobsFixture(t, upstream.URL, "openai")
			incomingPath := path
			if path == "legacy-empty" {
				incomingPath = "/v1/responses"
			}
			backgroundPathTarget(h, incomingPath)
			h.cfg.RequestGuard = func(_ context.Context, req *gateway.Request) error {
				checks.Add(1)
				if req.Path != incomingPath {
					t.Error("admission lost original caller path")
				}
				return nil
			}
			const original = `{"model":"gpt-test","input":"hello","background":true}`
			id := createBackgroundForTest(t, wrapped, incomingPath, original)
			job, err := repo.GetOwned(context.Background(), id, 1, 11)
			if err != nil || job.Path != incomingPath || string(job.Body) != original {
				t.Fatalf("creation did not freeze source path/body job=%+v err=%v", job, err)
			}
			if path == "legacy-empty" {
				repo.mu.Lock()
				job.Path = ""
				repo.jobs[id] = job
				repo.mu.Unlock()
				data, _ := json.Marshal(job)
				if gjson.GetBytes(data, "path").Exists() {
					t.Fatalf("empty path changes legacy/imported record shape %s", data)
				}
			}
			restartedCfg := h.cfg
			restartedCfg.RequestGuard = func(context.Context, *gateway.Request) error {
				t.Error("worker repeated incoming admission")
				return &gateway.UpstreamError{Status: 429, Code: "denied"}
			}
			restarted, err := New(restartedCfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := restarted.Reconcile(context.Background(), 1); err != nil {
				t.Fatal(err)
			}
			out, count, _ := billing.result(id)
			if checks.Load() != 1 || calls.Load() != 1 || !out.Charge || count != 1 {
				t.Fatalf("worker repeated admission/lost path checks=%d upstream=%d outcome=%+v count=%d", checks.Load(), calls.Load(), out, count)
			}
		})
	}
}

func TestBackgroundOriginalPathSurvivesEncryptedRedisRestore(t *testing.T) {
	repo, client := backgroundRedisTest(t)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if gjson.GetBytes(body, "metadata.source").Str != "preserved" {
			t.Errorf("Redis restored worker lost request path: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, backgroundCompletedSnapshot)
	}))
	defer upstream.Close()
	h, _, _, _, billing := backgroundJobsFixture(t, upstream.URL, "openai")
	h.cfg.BackgroundJobs = repo
	backgroundPathTarget(h, "/backend-api/codex/responses")
	id := createBackgroundForTest(t, h.Wrap(http.NewServeMux()), "/backend-api/codex/responses", `{"model":"gpt-test","input":"hello","background":true}`)
	// A fresh repository/handler can only recover the path from encrypted Redis.
	restartedRepo := &RedisBackgroundRepository{client: client, prefix: repo.prefix, aead: repo.aead}
	job, err := restartedRepo.GetOwned(context.Background(), id, 1, 11)
	if err != nil || job.Path != "/backend-api/codex/responses" {
		t.Fatalf("Redis record dropped original path %+v %v", job, err)
	}
	cfg := h.cfg
	cfg.BackgroundJobs = restartedRepo
	cfg.RequestGuard = func(context.Context, *gateway.Request) error { t.Error("worker consumed admission"); return nil }
	restarted, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Reconcile(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	out, count, _ := billing.result(id)
	if calls.Load() != 1 || !out.Charge || count != 1 {
		t.Fatalf("restored execution failed calls=%d out=%+v charges=%d", calls.Load(), out, count)
	}
}

func TestFileAndBackgroundMetadataDoNotConsumeRequestGuard(t *testing.T) {
	h, _, wrapped, _, _ := backgroundJobsFixture(t, "https://unused.invalid", "openai")
	id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true}`)
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 1, []byte("file bytes"))
	h.cfg.Files = store
	var checks atomic.Int32
	h.cfg.RequestGuard = func(context.Context, *gateway.Request) error {
		checks.Add(1)
		return &gateway.UpstreamError{Status: 429, Code: "rate_limited"}
	}
	mux := http.NewServeMux()
	h.Register(mux)
	for _, path := range []string{"/responses/" + id, "/v1/files", "/v1/files/" + file.ID, "/v1/files/" + file.ID + "/content"} {
		w := filesTestRequest(mux, http.MethodGet, path, "owner", nil, "")
		if w.Code != 200 {
			t.Fatalf("metadata read unexpectedly denied path=%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
	w := filesTestUpload(t, mux, "owner", []byte("new bytes"), "new.txt", "user_data")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "file") || checks.Load() != 0 {
		t.Fatalf("file/metadata traffic consumed generation guard status=%d body=%s checks=%d", w.Code, w.Body.String(), checks.Load())
	}
}
