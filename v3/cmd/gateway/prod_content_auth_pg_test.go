//go:build pgintegration

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
)

func TestProductionBrowserSessionReadsOnlyOwnedVideoContent(t *testing.T) {
	secret := bytes.Repeat([]byte{0x52}, 32)
	t.Setenv("V3_SESSION_SECRET", base64.StdEncoding.EncodeToString(secret))
	t.Setenv("V3_FILES_DIR", t.TempDir())
	f := newContractFixture(t)
	ctx := context.Background()
	control, err := identity.NewControl(f.deps.PG.Pool, identity.ControlConfig{
		SessionSecret: secret, EncryptionKey: f.deps.Crypto.DeriveKey("fixture-control"),
	}, runtimeLogger())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := control.NewSession(ctx, identity.User{ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.deps.PG.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(2,'foreign-content-user')`); err != nil {
		t.Fatal(err)
	}
	foreign, err := control.NewSession(ctx, identity.User{ID: 2})
	if err != nil {
		t.Fatal(err)
	}
	var upstreamCalls atomic.Int64
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		if r.URL.Path != "/v1/videos/native-video/content" || r.Header.Get("Authorization") != "Bearer upstream-test-key" {
			t.Errorf("video upstream path=%q authenticated=%v", r.URL.Path, r.Header.Get("Authorization") == "Bearer upstream-test-key")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = io.WriteString(w, "fixture-video-bytes")
	}))
	t.Cleanup(media.Close)
	if _, err = f.deps.PG.Exec(ctx, `UPDATE v3_catalog.channels SET base_url=$1 WHERE id=1`, media.URL); err != nil {
		t.Fatal(err)
	}
	task := workflow.Task{ID: "task_browser_content", UserID: 1, KeyID: 1, Group: "default", Provider: "openai",
		ChannelID: 1, CredentialID: 1, Model: "contract-model", UpstreamModel: "contract-model", Action: "generate",
		Status: "submitting", Body: []byte(`{}`), CreatedAt: time.Now().UTC()}
	repository := &workflow.PostgresRepository{Pool: f.deps.PG.Pool}
	if err = repository.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if _, err = f.deps.PG.Exec(ctx, `UPDATE v3_workflow.tasks SET status='completed',upstream_id='native-video',cost_state='settled' WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(`{"model":"contract-model"}`))
		r.RemoteAddr = "127.0.0.1:3456"
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: "codego_session", Value: token})
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	contentPath := "/v1/videos/" + task.ID + "/content"
	w := request("GET", contentPath, owner.AccessToken)
	if w.Code != 200 || w.Body.String() != "fixture-video-bytes" || w.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("owned browser content status=%d body=%s", w.Code, w.Body)
	}
	w = request("GET", contentPath, foreign.AccessToken)
	if w.Code != 404 {
		t.Fatalf("foreign browser content status=%d body=%s", w.Code, w.Body)
	}
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/v1/videos/" + task.ID}, {"POST", "/v1/videos"}, {"POST", "/v1/chat/completions"},
	} {
		w = request(endpoint.method, endpoint.path, owner.AccessToken)
		if w.Code != 401 {
			t.Errorf("session-only %s %s status=%d body=%s", endpoint.method, endpoint.path, w.Code, w.Body)
		}
	}
	if _, err = f.deps.PG.Exec(ctx, `UPDATE v3_identity.sessions SET revoked_at=now() WHERE user_id=1`); err != nil {
		t.Fatal(err)
	}
	w = request("GET", contentPath, owner.AccessToken)
	if w.Code != 401 {
		t.Fatalf("revoked session content status=%d", w.Code)
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("rejected browser requests contacted upstream: calls=%d", upstreamCalls.Load())
	}
}
