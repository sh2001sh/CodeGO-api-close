package kling

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestDirectCallsApplyChannelOverridesAndKeepGetBodyless(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Configured") != "channel" || r.Header.Get("Authorization") != "Bearer sk-fixture" {
			t.Error("channel headers missing")
		}
		if r.Method == http.MethodPost {
			var fields map[string]any
			if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
				t.Error(err)
			}
			if fields["model_name"] != "configured-model" || fields["prompt"] != "original" || fields["duration"] != "10" {
				t.Errorf("channel body missing: %v", fields)
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"task"}}`))
		} else {
			body, err := io.ReadAll(r.Body)
			if err != nil || len(body) != 0 || r.Header.Get("Content-Type") != "" {
				t.Error("GET poll must stay bodyless despite configured JSON body overrides")
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"task","task_status":"succeed","task_result":{"videos":[{"url":"https://media.example/video","duration":"10"}]}}}`))
		}
	}))
	defer server.Close()
	target := gateway.Target{BaseURL: server.URL, Secret: "sk-fixture", UpstreamModel: "mapped-model", ParamOverride: map[string]any{"model_name": "configured-model", "duration": "10"}, HeaderOverride: map[string]string{"X-Configured": "channel"}, StatusCodeMapping: map[string]int{"503": 200}}
	p := New(server.Client())
	body := []byte(`{"prompt":"original","model":"client-model","duration":5}`)
	before := bytes.Clone(body)
	r, err := p.Submit(context.Background(), target, native.Submit{Body: body})
	if err != nil || r.ID != "task" {
		t.Fatalf("submit %+v %v", r, err)
	}
	r, err = p.Poll(context.Background(), target, native.Task{UpstreamID: r.ID, Data: r.Data})
	if err != nil || r.Status != "completed" || r.Units != 10 || calls != 2 {
		t.Fatalf("poll %+v %v, calls %d", r, err, calls)
	}
	if !bytes.Equal(body, before) {
		t.Error("client body mutated")
	}
}
