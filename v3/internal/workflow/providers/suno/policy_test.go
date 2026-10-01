package suno

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestDirectCallsApplyChannelPolicyAndStatusMapping(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Configured") != "channel" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("channel headers missing")
		}
		var fields map[string]any
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			t.Error(err)
		}
		if fields["title"] != "configured title" {
			t.Errorf("parameters missing: %v", fields)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		if r.URL.Path == "/suno/submit/MUSIC" {
			if fields["mv"] != "chirp-configured" || fields["prompt"] != "original" {
				t.Errorf("native conversion changed: %v", fields)
			}
			_, _ = w.Write([]byte(`{"code":"success","data":"task"}`))
		} else {
			_, _ = w.Write([]byte(`{"code":"success","data":[{"task_id":"task","status":"processing"}]}`))
		}
	}))
	defer server.Close()
	target := gateway.Target{BaseURL: server.URL, Secret: "fixture-key", UpstreamModel: "chirp-configured", ParamOverride: map[string]any{"title": "configured title"}, HeaderOverride: map[string]string{"X-Configured": "channel"}, StatusCodeMapping: map[string]int{"503": 200}}
	p := New(server.Client())
	body := []byte(`{"prompt":"original","title":"client title"}`)
	before := bytes.Clone(body)
	r, err := p.Submit(context.Background(), target, native.Submit{Action: "music", Body: body})
	if err != nil || r.ID != "task" {
		t.Fatalf("submit %+v %v", r, err)
	}
	r, err = p.Poll(context.Background(), target, native.Task{UpstreamID: r.ID})
	if err != nil || r.Status != "in_progress" || calls != 2 {
		t.Fatalf("poll %+v %v, calls %d", r, err, calls)
	}
	if !bytes.Equal(body, before) {
		t.Error("client body mutated")
	}
}
