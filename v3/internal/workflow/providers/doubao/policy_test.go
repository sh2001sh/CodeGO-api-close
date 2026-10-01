package doubao

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
		if r.Header.Get("X-Policy") != "applied" || r.Header.Get("Authorization") != "Bearer credential" {
			t.Error("target headers missing")
		}
		w.WriteHeader(503)
		if r.Method == http.MethodPost {
			var body struct {
				Model    string `json:"model"`
				Duration int    `json:"duration"`
				Content  []struct {
					Type string `json:"type"`
				} `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Duration != 12 || body.Model != "mapped" || len(body.Content) != 2 || body.Content[0].Type != "image_url" {
				t.Errorf("policy missed converted payload %+v", body)
			}
			_, _ = io.WriteString(w, `{"id":"id"}`)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil || len(data) != 0 || r.Header.Get("Content-Type") != "" {
			t.Errorf("GET parameters applied: %s %v", data, err)
		}
		_, _ = io.WriteString(w, `{"status":"succeeded","duration":12,"content":{"video_url":"https://video.test"}}`)
	}))
	defer server.Close()
	target := gateway.Target{BaseURL: server.URL, Secret: "credential", UpstreamModel: "mapped",
		ParamOverride: map[string]any{"duration": 12}, HeaderOverride: map[string]string{"X-Policy": "applied"}, StatusCodeMapping: map[string]int{"503": 200}}
	p := New(server.Client())
	input := native.Submit{Body: []byte(`{"prompt":"hello","image":"https://image.test","duration":"5"}`)}
	if result, err := p.Submit(context.Background(), target, input); err != nil || result.ID != "id" {
		t.Fatalf("submit %+v %v", result, err)
	}
	if result, err := p.Poll(context.Background(), target, native.Task{UpstreamID: "id"}); err != nil || result.Status != "completed" || result.Units != 12 {
		t.Fatalf("poll %+v %v", result, err)
	}
}
