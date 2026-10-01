package workflow_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestTaskResponsesRemoveCredentialsAndInternalRoutingMetadata(t *testing.T) {
	f := newAPIFixture(t)
	f.provider.submitFn = func(context.Context, gateway.Target, native.Submit) (native.Result, error) {
		return native.Result{ID: "native-secret", Status: "queued", Data: json.RawMessage(`{"task_id":"native-secret","api_key":"secret-marker","authorization":"Bearer secret-marker","details":{"message":"secret-marker"},"_v3_req_key":"stable-polling-model"}`)}, nil
	}
	w := f.request("POST", "/v1/videos", "owner", `{"model":"video"}`, "")
	if w.Code != 200 {
		t.Fatalf("submit %d %s", w.Code, w.Body.String())
	}
	task := f.repo.one(t)
	if strings.Contains(string(task.Data), "secret-marker") || strings.Contains(string(task.Data), "api_key") {
		t.Fatal("provider echoed credential persisted")
	}
	if !strings.Contains(string(task.Data), "stable-polling-model") {
		t.Fatal("poll routing metadata lost")
	}
	// Terminal tasks do not poll; inspect the client-facing task DTO through
	// the compatibility endpoint while retaining the exact provider metadata.
	task.Status = "completed"
	task.CostState = "settled"
	f.repo.seed(task)
	w = f.request("GET", "/v1/video/generations/"+task.ID, "owner", "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "native-secret") || strings.Contains(w.Body.String(), "_v3_") || strings.Contains(w.Body.String(), "secret-marker") {
		t.Fatalf("private data exposed %d %s", w.Code, w.Body.String())
	}
	stored := f.repo.one(t)
	if !strings.Contains(string(stored.Data), "native-secret") || !strings.Contains(string(stored.Data), "stable-polling-model") {
		t.Fatal("response conversion corrupted persisted native task")
	}
}

func TestTaskPricingHeadersExcludeCallerCredentialsAndSurviveReconcile(t *testing.T) {
	f := newAPIFixture(t)
	req := httptest.NewRequest("POST", "/v1/videos", strings.NewReader(`{"model":"video"}`))
	req.Header.Set("Authorization", "Bearer owner")
	req.Header.Set("X-Api-Key", "private-key")
	req.Header.Set("Cookie", "private-session")
	req.Header.Set("X-Pricing-Tier", "premium")
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("submit %d", w.Code)
	}
	task := f.repo.one(t)
	if task.PricingHeaders["X-Pricing-Tier"] != "premium" || task.Request().PricingHeaders["X-Pricing-Tier"] != "premium" {
		t.Fatal("pricing inputs changed after restore")
	}
	for _, key := range []string{"Authorization", "X-Api-Key", "Cookie"} {
		if _, ok := task.PricingHeaders[key]; ok {
			t.Fatalf("secret pricing header persisted %s", key)
		}
	}
}
