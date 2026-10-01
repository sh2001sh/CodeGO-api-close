package doubao

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestCompatibilityConversionAndMappedModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/contents/generations/tasks" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("incorrect submission request")
		}
		var body struct {
			Model         string `json:"model"`
			Duration      int    `json:"duration"`
			GenerateAudio bool   `json:"generate_audio"`
			Content       []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				Role     string `json:"role"`
				VideoURL struct {
					URL string `json:"url"`
				} `json:"video_url"`
			} `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "mapped" || body.Duration != 8 || !body.GenerateAudio {
			t.Errorf("incorrect conversion %+v", body)
		}
		if len(body.Content) != 2 || body.Content[0].Type != "video_url" || body.Content[0].VideoURL.URL != "https://video.test" || body.Content[0].Role != "reference_video" || body.Content[1].Text != "prompt" {
			t.Errorf("incorrect content %+v", body.Content)
		}
		_, _ = io.WriteString(w, `{"id":"id"}`)
	}))
	defer server.Close()
	body := `{"prompt":"prompt","seconds":"8","metadata":{"model":"override","generate_audio":true,"content":[{"type":"text","text":"old"},{"type":"video_url","video_url":{"url":"https://video.test"},"role":"reference_video"}]}}`
	r, err := New(server.Client()).Submit(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "secret", UpstreamModel: "mapped"}, native.Submit{Body: []byte(body)})
	if err != nil || r.ID != "id" || r.Status != "queued" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestNativeContentAndSingleImage(t *testing.T) {
	for _, test := range []struct{ body, kind string }{
		{`{"content":[{"type":"text","text":"native prompt"}],"model":"client"}`, "text"},
		{`{"prompt":"compat prompt","image":"https://image.test"}`, "image_url"},
		{`{"prompt":"compat prompt","metadata":"{\"content\":[{\"type\":\"audio_url\",\"audio_url\":{\"url\":\"https://audio.test\"}}]}"}`, "audio_url"},
	} {
		data, err := requestBody([]byte(test.body), "upstream")
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Model   string `json:"model"`
			Content []struct {
				Type string `json:"type"`
			} `json:"content"`
		}
		if json.Unmarshal(data, &body) != nil || body.Model != "upstream" || len(body.Content) == 0 || body.Content[0].Type != test.kind {
			t.Fatalf("incorrect conversion %s", data)
		}
	}
}

func TestFlexibleNativeNumberAndBooleanFields(t *testing.T) {
	data, err := requestBody([]byte(`{"prompt":"hello","duration":"10","metadata":{"frames":"120","seed":"7","generate_audio":"true","watermark":"false"}}`), "model")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	if body["duration"] != float64(10) || body["frames"] != float64(120) || body["seed"] != float64(7) || body["generate_audio"] != true || body["watermark"] != false {
		t.Fatalf("incorrect native coercion %s", data)
	}
}

func TestPollActualUsageAndFailures(t *testing.T) {
	cases := []struct {
		body, status string
		fails        bool
		code         int
	}{
		{`{"status":"running"}`, "in_progress", false, 200},
		{`{"status":"succeeded","duration":8,"content":{"video_url":"https://video.test"},"usage":{"completion_tokens":100,"total_tokens":120,"tool_usage":{"web_search":2}}}`, "completed", false, 200},
		{`{"status":"failed","error":{"message":"blocked"}}`, "failed", false, 200},
		{`{}`, "", true, 200},
		{`{"status":"unexpected"}`, "", true, 200},
		{`{"status":"succeeded","usage":{"completion_tokens":-1}}`, "", true, 200},
		{`broken json`, "", true, 200},
		{`{"error":{"message":"auth"}}`, "", true, 403},
	}
	for _, test := range cases {
		t.Run(test.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/v3/contents/generations/tasks/id" || r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("incorrect polling request")
				}
				w.WriteHeader(test.code)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			r, err := New(server.Client()).Poll(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "secret"}, native.Task{UpstreamID: "id"})
			if (err != nil) != test.fails {
				t.Fatalf("unexpected error %v", err)
			}
			if test.fails {
				return
			}
			if r.Status != test.status || r.ID != "id" || string(r.Data) != test.body {
				t.Fatalf("unexpected result %+v", r)
			}
			if r.Status == "completed" && (r.Units != 8 || r.URL != "https://video.test" || r.Usage.PromptTokens != 20 || r.Usage.CompletionTokens != 100 || r.Usage.ToolCalls["web_search"] != 2) {
				t.Fatalf("missing usage %+v", r)
			}
			if r.Status == "failed" && r.Error != "blocked" {
				t.Error("missing failure reason")
			}
		})
	}
}

func TestInvalidRequests(t *testing.T) {
	for _, body := range []string{`null`, `{`, `{"content":123}`, `{"metadata":[]}`, `{"images":123}`, `{"prompt":123}`, `{"seconds":12}`, `{"prompt":"hello","duration":"invalid"}`, `{"prompt":"hello","generate_audio":"invalid"}`} {
		if _, err := requestBody([]byte(body), "model"); err == nil {
			t.Errorf("invalid request accepted %s", body)
		}
	}
	p := New(nil)
	if _, err := p.Poll(context.Background(), gateway.Target{}, native.Task{}); err == nil {
		t.Fatal("empty task ID accepted")
	}
	if _, err := p.Content(context.Background(), gateway.Target{}, native.Task{}); !errors.Is(err, native.ErrContentUnsupported) {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestSubmitErrorClassification(t *testing.T) {
	var invalid *native.InvalidRequest
	if _, err := New(nil).Submit(context.Background(), gateway.Target{UpstreamModel: "model"}, native.Submit{Body: []byte(`null`)}); !errors.As(err, &invalid) {
		t.Fatalf("local validation must be refundable: %v", err)
	}
	for _, test := range []struct {
		body     string
		code     int
		rejected bool
	}{
		{`{"error":{"message":"bad request"}}`, 200, true},
		{`{"error":"auth"}`, 401, true},
		{`{}`, 200, false},
		{`malformed`, 200, false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(test.code)
			_, _ = io.WriteString(w, test.body)
		}))
		_, err := New(server.Client()).Submit(context.Background(), gateway.Target{BaseURL: server.URL, UpstreamModel: "model"}, native.Submit{Body: []byte(`{"prompt":"hello"}`)})
		server.Close()
		var rejected *native.Rejected
		if err == nil || errors.As(err, &rejected) != test.rejected || errors.As(err, &invalid) {
			t.Fatalf("incorrect rejection classification %s: %v", test.body, err)
		}
	}
}
