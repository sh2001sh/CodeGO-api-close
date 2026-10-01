package suno

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestSubmitAndPollPreserveTaskAndDuration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" || r.Method != "POST" {
			t.Errorf("invalid auth or method")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		switch r.URL.Path {
		case "/suno/submit/MUSIC":
			if body["mv"] != "chirp-v4-mapped" || body["prompt"] != "a song" || body["title"] != "title" {
				t.Errorf("unexpected submit: %v", body)
			}
			if _, ok := body["model"]; ok {
				t.Error("client model leaked")
			}
			_, _ = w.Write([]byte(`{"code":"success","data":"upstream-task"}`))
		case "/suno/fetch":
			if !reflect.DeepEqual(body["ids"], []any{"upstream-task"}) {
				t.Errorf("unexpected fetch: %v", body)
			}
			_, _ = w.Write([]byte(`{"code":"success","data":[{"task_id":"other","status":"processing"},{"task_id":"upstream-task","status":"success","data":[{"audio_url":"https://media.example/a","metadata":{"duration":120.5}},{"audio_url":"https://media.example/b","metadata":{"duration":"90"}}]}]}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	p := New(server.Client())
	target := gateway.Target{BaseURL: server.URL, Secret: "fixture-key", UpstreamModel: "chirp-v4-mapped"}
	result, err := p.Submit(context.Background(), target, native.Submit{Action: "music", Model: "suno_music", Body: []byte(`{"model":"suno_music","mv":"client-mv","prompt":"a song","title":"title"}`)})
	if err != nil || result.ID != "upstream-task" || result.Status != "queued" {
		t.Fatalf("submit: %+v %v", result, err)
	}
	result, err = p.Poll(context.Background(), target, native.Task{UpstreamID: result.ID, Action: "MUSIC"})
	if err != nil || result.ID != "upstream-task" || result.Status != "completed" || result.URL != "https://media.example/a" || result.Units != 210.5 {
		t.Fatalf("poll: %+v %v", result, err)
	}
	if !strings.Contains(string(result.Data), `"task_id":"upstream-task"`) {
		t.Error("native task data not preserved")
	}
}

func TestLyricsAndValidationAreDefinitive(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/suno/submit/LYRICS" {
			t.Errorf("wrong action %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":"failure","message":"invalid prompt"}`))
	}))
	defer server.Close()
	p := New(server.Client())
	target := gateway.Target{BaseURL: server.URL, Secret: "fixture-key"}
	for _, input := range []native.Submit{{Action: "other", Body: []byte(`{}`)}, {Action: "lyrics", Body: []byte(`{}`)}, {Action: "music", Body: []byte(`null`)}} {
		_, err := p.Submit(context.Background(), target, input)
		var invalid *native.InvalidRequest
		if !errors.As(err, &invalid) {
			t.Fatalf("expected local invalid request, got %v", err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid request reached upstream")
	}
	r, err := p.Submit(context.Background(), target, native.Submit{Action: "lyrics", Body: []byte(`{"prompt":"one line"}`)})
	if err != nil || r.Status != "failed" || r.Error != "invalid prompt" || calls != 1 {
		t.Fatalf("rejection: %+v %v", r, err)
	}
}

func TestPollFailureMissingTaskAndUnknownStatus(t *testing.T) {
	for _, tc := range []struct {
		name, body, status string
		wantErr            bool
	}{
		{"failed", `{"code":"success","data":[{"task_id":"id","status":"failed","fail_reason":"generation failed","data":{}}]}`, "failed", false},
		{"processing", `{"code":"success","data":[{"task_id":"id","status":"processing"}]}`, "in_progress", false},
		{"missing", `{"code":"success","data":[]}`, "", true},
		{"unknown", `{"code":"success","data":[{"task_id":"id","status":"new-state"}]}`, "", true},
		{"rejected-fetch", `{"code":"failed","message":"temporary"}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer server.Close()
			r, err := New(server.Client()).Poll(context.Background(), gateway.Target{BaseURL: server.URL}, native.Task{UpstreamID: "id"})
			if (err != nil) != tc.wantErr || r.Status != tc.status {
				t.Fatalf("result %+v, error %v", r, err)
			}
		})
	}
}

func TestHTTPRejectAndMalformedAcceptance(t *testing.T) {
	for _, tc := range []struct {
		status   int
		body     string
		rejected bool
	}{
		{400, `{"error":"bad request"}`, true}, {200, `{"code":"success","data":""}`, false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		_, err := New(server.Client()).Submit(context.Background(), gateway.Target{BaseURL: server.URL}, native.Submit{Action: "music", Body: []byte(`{}`)})
		server.Close()
		var rejected *native.Rejected
		var invalid *native.InvalidRequest
		if err == nil || errors.As(err, &rejected) != tc.rejected || errors.As(err, &invalid) {
			t.Fatalf("incorrect error category: %v", err)
		}
	}
}
