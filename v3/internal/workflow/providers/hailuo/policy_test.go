package hailuo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestDirectSubmitPollAndRetrieveApplyTargetPolicy(t *testing.T) {
	var retrieved atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Policy") != "applied" || r.Header.Get("Authorization") != "Bearer credential" {
			t.Error("target headers missing")
		}
		w.WriteHeader(503)
		if r.Method == http.MethodPost {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["duration"] != float64(12) || body["model"] != "mapped" || body["resolution"] != "1080P" {
				t.Errorf("policy missed converted payload %+v", body)
			}
			_, _ = io.WriteString(w, `{"task_id":"id","base_resp":{"status_code":0}}`)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil || len(data) != 0 || r.Header.Get("Content-Type") != "" {
			t.Errorf("GET parameters applied: %s %v", data, err)
		}
		if r.URL.Path == "/v1/files/retrieve" {
			retrieved.Add(1)
			_, _ = io.WriteString(w, `{"file":{"download_url":"https://video.test"},"base_resp":{"status_code":0}}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"Success","duration":12,"file_id":"file","base_resp":{"status_code":0}}`)
	}))
	defer server.Close()
	target := gateway.Target{BaseURL: server.URL, Secret: "credential", UpstreamModel: "mapped",
		ParamOverride: map[string]any{"duration": 12}, HeaderOverride: map[string]string{"X-Policy": "applied"}, StatusCodeMapping: map[string]int{"503": 200}}
	p := New(server.Client())
	input := native.Submit{Body: []byte(`{"prompt":"hello","size":"1920x1080","duration":"5"}`)}
	if result, err := p.Submit(context.Background(), target, input); err != nil || result.ID != "id" {
		t.Fatalf("submit %+v %v", result, err)
	}
	if result, err := p.Poll(context.Background(), target, native.Task{UpstreamID: "id"}); err != nil || result.Status != "completed" || result.Units != 12 || result.URL != "https://video.test" || retrieved.Load() != 1 {
		t.Fatalf("poll %+v %v", result, err)
	}
}
