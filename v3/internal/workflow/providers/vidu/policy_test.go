package vidu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestDirectSubmitAndPollApplyTargetPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Policy") != "applied" || r.Header.Get("Authorization") != "Token credential" {
			t.Error("target headers missing")
		}
		w.WriteHeader(503)
		if r.Method == http.MethodPost {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["duration"] != float64(12) || body["model"] != "mapped" || body["images"] == nil {
				t.Errorf("policy missed converted payload %+v", body)
			}
			_, _ = io.WriteString(w, `{"task_id":"id","state":"created"}`)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil || len(data) != 0 || r.Header.Get("Content-Type") != "" {
			t.Errorf("GET parameters applied: %s %v", data, err)
		}
		_, _ = io.WriteString(w, `{"state":"success","duration":12,"creations":[{"url":"https://video.test"}]}`)
	}))
	defer server.Close()
	target := gateway.Target{BaseURL: server.URL, Secret: "credential", UpstreamModel: "mapped",
		ParamOverride: map[string]any{"duration": 12}, HeaderOverride: map[string]string{"X-Policy": "applied"}, StatusCodeMapping: map[string]int{"503": 200}}
	p := New(server.Client())
	input := native.Submit{Body: []byte(`{"prompt":"hello","image":"https://image.test","duration":5}`)}
	if result, err := p.Submit(context.Background(), target, input); err != nil || result.ID != "id" {
		t.Fatalf("submit %+v %v", result, err)
	}
	if result, err := p.Poll(context.Background(), target, native.Task{UpstreamID: "id"}); err != nil || result.Status != "completed" || result.Units != 12 {
		t.Fatalf("poll %+v %v", result, err)
	}
}
