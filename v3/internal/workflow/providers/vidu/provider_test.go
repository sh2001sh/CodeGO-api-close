package vidu

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

func TestSubmissionRoutesAndMetadata(t *testing.T) {
	cases := []struct{ body, action, path string }{
		{`{"prompt":"hello","duration":"8","size":"720p","metadata":{"model":"evil","bgm":true,"seed":7}}`, "", "text2video"},
		{`{"prompt":"hello","image":"https://image.test"}`, "generate", "img2video"},
		{`{"prompt":"hello","images":["first","last"]}`, "generate", "start-end2video"},
		{`{"prompt":"hello","images":["first","second","third"]}`, "", "reference2video"},
		{`{"prompt":"hello","images":["first","second","third"],"metadata":{"action":"generate"}}`, "", "img2video"},
		{`{"prompt":"hello","metadata":"{\"action\":\"textGenerate\"}"}`, "", "text2video"},
	}
	for _, test := range cases {
		t.Run(test.path+test.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/ent/v2/"+test.path || r.Header.Get("Authorization") != "Token secret" {
					t.Errorf("incorrect request %s %s %s", r.Method, r.URL.Path, r.Header.Get("Authorization"))
				}
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("invalid JSON")
				}
				if body["model"] != "mapped" || body["prompt"] != "hello" {
					t.Errorf("unexpected body %+v", body)
				}
				if _, ok := body["metadata"]; ok {
					t.Error("unconverted metadata")
				}
				if test.path == "text2video" && body["seed"] != nil && (body["duration"] != float64(8) || body["resolution"] != "720p" || body["bgm"] != true) {
					t.Errorf("metadata/default conversion %+v", body)
				}
				_, _ = io.WriteString(w, `{"task_id":"id","state":"created"}`)
			}))
			defer server.Close()
			r, err := New(server.Client()).Submit(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "secret", UpstreamModel: "mapped"}, native.Submit{Action: test.action, Body: []byte(test.body)})
			if err != nil || r.ID != "id" || r.Status != "queued" {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
}

func TestPollAndFailures(t *testing.T) {
	cases := []struct {
		body, status string
		units        float64
		fails        bool
		code         int
	}{
		{`{"state":"processing"}`, "in_progress", 0, false, 200},
		{`{"state":"success","duration":5,"creations":[{"url":"https://video.test","duration":8}]}`, "completed", 8, false, 200},
		{`{"state":"failed","err_code":"blocked"}`, "failed", 0, false, 200},
		{`{"state":"new-state"}`, "", 0, true, 200},
		{`{}`, "", 0, true, 200},
		{`{"state":"success","duration":-1}`, "", 0, true, 200},
		{`not json`, "", 0, true, 200},
		{`{"err_code":"unauthorized"}`, "", 0, true, 401},
	}
	for _, test := range cases {
		t.Run(test.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/ent/v2/tasks/id/creations" || r.Header.Get("Authorization") != "Token secret" {
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
			if !test.fails && (r.Status != test.status || r.Units != test.units || r.ID != "id" || string(r.Data) != test.body) {
				t.Fatalf("unexpected result %+v", r)
			}
			if r.Status == "completed" && r.URL != "https://video.test" {
				t.Error("result URL missing")
			}
			if r.Status == "failed" && r.Error != "blocked" {
				t.Error("failure reason missing")
			}
		})
	}
}

func TestInvalidRequestAndUnsupportedContent(t *testing.T) {
	for _, body := range []string{`null`, `{`, `{"images":123}`, `{"metadata":[]}`, `{"duration":"bad"}`} {
		if _, _, err := requestBody(native.Submit{Body: []byte(body)}, "model"); err == nil {
			t.Errorf("accepted invalid request %s", body)
		}
	}
	if _, _, err := requestBody(native.Submit{Body: []byte(`{}`)}, ""); err == nil {
		t.Fatal("empty model accepted")
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
		{`{"state":"failed","err_code":"bad_request"}`, 200, true},
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
