package workflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type apiAuth struct{}

func (apiAuth) Authorize(_ context.Context, key string) (gateway.Principal, error) {
	if key == "unavailable" {
		return gateway.Principal{}, gateway.ErrAuthUnavailable
	}
	user := int64(11)
	if key == "other" {
		user = 22
	} else if key != "owner" {
		return gateway.Principal{}, errors.New("invalid")
	}
	return gateway.Principal{UserID: user, KeyID: user + 100, Group: "default"}, nil
}

type apiPlanner struct{ target gateway.Target }

func (p apiPlanner) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	return []gateway.Target{p.target}, nil
}
func (apiPlanner) Report(gateway.Target, gateway.AttemptResult) {}

type apiRepository struct {
	mu           sync.Mutex
	tasks        map[string]workflow.Task
	until        map[string]time.Time
	createErr    error
	saveFailures int
}

func (r *apiRepository) Create(_ context.Context, task workflow.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return r.createErr
	}
	if _, ok := r.tasks[task.ID]; ok {
		return workflow.ErrConflict
	}
	r.tasks[task.ID], r.until[task.ID] = task, time.Now().Add(5*time.Minute)
	return nil
}
func (r *apiRepository) GetOwned(_ context.Context, id string, user int64) (workflow.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[id]
	if !ok || task.UserID != user {
		return workflow.Task{}, workflow.ErrNotFound
	}
	return task, nil
}
func eligible(task workflow.Task) bool {
	if task.CostState != "reserved" {
		return false
	}
	return task.Status == "queued" || task.Status == "in_progress" || task.Status == "completed" || task.Status == "failed"
}
func (r *apiRepository) Pending(_ context.Context, limit int) ([]workflow.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []workflow.Task{}
	for id, task := range r.tasks {
		if eligible(task) && !r.until[id].After(time.Now()) {
			out = append(out, task)
		}
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
func (r *apiRepository) Claim(_ context.Context, id, lease string, until time.Time) (workflow.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[id]
	if !ok || !eligible(task) || r.until[id].After(time.Now()) {
		return workflow.Task{}, workflow.ErrConflict
	}
	task.LeaseID = lease
	r.tasks[id], r.until[id] = task, until
	return task, nil
}
func (r *apiRepository) Save(_ context.Context, task workflow.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.tasks[task.ID]
	if !ok || stored.LeaseID != task.LeaseID {
		return workflow.ErrConflict
	}
	if r.saveFailures > 0 {
		r.saveFailures--
		return errors.New("storage unavailable")
	}
	task.LeaseID = ""
	r.tasks[task.ID], r.until[task.ID] = task, time.Time{}
	return nil
}
func (r *apiRepository) seed(task workflow.Task) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks[task.ID] = task
}
func (r *apiRepository) one(t *testing.T) workflow.Task {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.tasks) != 1 {
		t.Fatalf("persisted task count = %d, want 1", len(r.tasks))
	}
	for _, task := range r.tasks {
		return task
	}
	panic("unreachable")
}
func (r *apiRepository) expireLeases() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id := range r.until {
		r.until[id] = time.Time{}
	}
}

type apiFinalize struct {
	id          string
	result      native.Result
	reservation workflow.Reservation
}
type apiSettler struct {
	mu        sync.Mutex
	reserved  []string
	calls     []apiFinalize
	committed map[string]credits.Micro
	debits    int
	failures  int
}

func (s *apiSettler) Reserve(_ context.Context, req *gateway.Request) (workflow.Reservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reserved = append(s.reserved, req.ID)
	data, _ := json.Marshal(map[string]string{"operation_id": req.ID})
	return workflow.Reservation{Data: data, EstimatedCredits: 90}, nil
}
func (s *apiSettler) Finalize(_ context.Context, req *gateway.Request, reservation workflow.Reservation, result native.Result) (credits.Micro, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, apiFinalize{req.ID, result, reservation})
	if s.failures > 0 {
		s.failures--
		return 0, errors.New("settlement unavailable")
	}
	if actual, ok := s.committed[req.ID]; ok {
		return actual, nil
	}
	actual := credits.Micro(0)
	if result.Status == "completed" {
		actual = 37
		s.debits++
	}
	s.committed[req.ID] = actual
	return actual, nil
}

type apiProvider struct {
	submitFn  func(context.Context, gateway.Target, native.Submit) (native.Result, error)
	pollFn    func(context.Context, gateway.Target, native.Task) (native.Result, error)
	contentFn func(context.Context, gateway.Target, native.Task) (*http.Response, error)
	submits   atomic.Int32
	polls     atomic.Int32
	contents  atomic.Int32
}

func (p *apiProvider) Submit(ctx context.Context, target gateway.Target, in native.Submit) (native.Result, error) {
	p.submits.Add(1)
	if p.submitFn != nil {
		return p.submitFn(ctx, target, in)
	}
	return native.Result{ID: "upstream-1", Status: "queued"}, nil
}
func (p *apiProvider) Poll(ctx context.Context, target gateway.Target, task native.Task) (native.Result, error) {
	p.polls.Add(1)
	if p.pollFn != nil {
		return p.pollFn(ctx, target, task)
	}
	return native.Result{ID: task.UpstreamID, Status: "completed", Units: 8,
		Usage: gateway.Usage{PromptTokens: 3, CompletionTokens: 17}}, nil
}
func (p *apiProvider) Content(ctx context.Context, target gateway.Target, task native.Task) (*http.Response, error) {
	p.contents.Add(1)
	if p.contentFn != nil {
		return p.contentFn(ctx, target, task)
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"video/mp4"}}, Body: io.NopCloser(strings.NewReader("video-content"))}, nil
}

type apiFixture struct {
	handler  *workflow.Handler
	mux      *http.ServeMux
	repo     *apiRepository
	settler  *apiSettler
	provider *apiProvider
	target   gateway.Target
}

func newAPIFixture(t *testing.T, edit ...func(*workflow.Config)) *apiFixture {
	t.Helper()
	f := &apiFixture{repo: &apiRepository{tasks: map[string]workflow.Task{}, until: map[string]time.Time{}},
		settler: &apiSettler{committed: map[string]credits.Micro{}}, provider: &apiProvider{},
		target: gateway.Target{ChannelID: 4, CredentialID: 9, Provider: "openai_video", Secret: "secret-marker", UpstreamModel: "native-video"}}
	cfg := workflow.Config{Authorizer: apiAuth{}, Planner: apiPlanner{f.target}, Settler: f.settler,
		Repository: f.repo, ResolveTarget: func(_ context.Context, channel, credential int64) (gateway.Target, error) {
			if channel != f.target.ChannelID || credential != f.target.CredentialID {
				return gateway.Target{}, errors.New("unknown credential")
			}
			return f.target, nil
		}, Providers: map[string]native.Adapter{"openai_video": f.provider, "suno": f.provider},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, change := range edit {
		change(&cfg)
	}
	var err error
	f.handler, err = workflow.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.mux = http.NewServeMux()
	f.handler.Register(f.mux)
	return f
}
func (f *apiFixture) request(method, path, key, body, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if contentType == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, req)
	return w
}
func (f *apiFixture) submit(t *testing.T) workflow.Task {
	t.Helper()
	w := f.request("POST", "/v1/videos", "owner", `{"model":"video","prompt":"sea"}`, "")
	if w.Code != 200 {
		t.Fatalf("submit = %d %s", w.Code, w.Body.String())
	}
	return f.repo.one(t)
}
func ownedTask(id, provider string) workflow.Task {
	return workflow.Task{ID: id, UserID: 11, KeyID: 111, Group: "default", Provider: provider,
		ChannelID: 4, CredentialID: 9, Model: "video", UpstreamModel: "native-video", UpstreamID: "native-" + id,
		Status: "completed", CostState: "settled", CreatedAt: time.Now(), UpdatedAt: time.Now()}
}
