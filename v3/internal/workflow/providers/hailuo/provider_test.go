package hailuo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestSubmitConversionAndModelAuthority(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/video_generation" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("incorrect request")
		}
		var body struct {
			Model      string `json:"model"`
			Prompt     string `json:"prompt"`
			Duration   int    `json:"duration"`
			Resolution string `json:"resolution"`
			First      string `json:"first_frame_image"`
			Optimizer  bool   `json:"prompt_optimizer"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "MiniMax-Hailuo-02" || body.Prompt != "hello" || body.Duration != 10 || body.Resolution != "1080P" || body.First != "https://image.test" || body.Optimizer {
			t.Errorf("unexpected body %+v", body)
		}
		_, _ = io.WriteString(w, `{"task_id":"id","base_resp":{"status_code":0}}`)
	}))
	defer server.Close()
	body := `{"prompt":"hello","size":"1920x1080","duration":"10","metadata":{"model":"evil","first_frame_image":"https://image.test","prompt_optimizer":false}}`
	r, err := New(server.Client()).Submit(context.Background(), gateway.Target{BaseURL: server.URL + "/v1", Secret: "secret", UpstreamModel: "MiniMax-Hailuo-02"}, native.Submit{Body: []byte(body)})
	if err != nil || r.ID != "id" || r.Status != "queued" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestPollRetrievesAuthenticatedFileAndPreservesTask(t *testing.T) {
	taskResponse := `{"task_id":"id","status":"Success","file_id":"file/id","duration":10,"base_resp":{"status_code":0}}`
	var queries, retrievals atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("incorrect auth/method")
		}
		switch r.URL.Path {
		case "/v1/query/video_generation":
			queries.Add(1)
			if r.URL.Query().Get("task_id") != "task/id" {
				t.Error("incorrect task query")
			}
			_, _ = io.WriteString(w, taskResponse)
		case "/v1/files/retrieve":
			retrievals.Add(1)
			if r.URL.Query().Get("file_id") != "file/id" {
				t.Error("incorrect file query")
			}
			_, _ = io.WriteString(w, `{"file":{"download_url":"https://video.test"},"base_resp":{"status_code":0}}`)
		default:
			t.Errorf("incorrect endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	r, err := New(server.Client()).Poll(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "secret"}, native.Task{UpstreamID: "task/id"})
	if err != nil || r.Status != "completed" || r.URL != "https://video.test" || r.Units != 10 || string(r.Data) != taskResponse || queries.Load() != 1 || retrievals.Load() != 1 {
		t.Fatalf("%+v %v queries=%d retrieves=%d", r, err, queries.Load(), retrievals.Load())
	}
}

func TestFailureNeverRetrievesOrBecomesSuccess(t *testing.T) {
	cases := []struct {
		body, status string
		fails        bool
		code         int
	}{
		{`{"status":"Queueing","base_resp":{"status_code":0}}`, "queued", false, 200},
		{`{"status":"Processing","base_resp":{"status_code":0}}`, "in_progress", false, 200},
		{`{"status":"Fail","base_resp":{"status_code":0,"status_msg":"blocked"}}`, "failed", false, 200},
		{`{"status":"Fail","base_resp":{"status_code":1026,"status_msg":"blocked"}}`, "failed", false, 200},
		{`{"status":"Success","file_id":"id","base_resp":{"status_code":1004,"status_msg":"auth error"}}`, "", true, 200},
		{`{"status":"Success","base_resp":{"status_code":0}}`, "", true, 200},
		{`{"status":"NewStatus","base_resp":{"status_code":0}}`, "", true, 200},
		{`{}`, "", true, 200},
		{`not json`, "", true, 200},
		{`{"base_resp":{"status_code":1004}}`, "", true, 401},
	}
	for _, test := range cases {
		t.Run(test.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/query/video_generation" {
					t.Error("unexpected file retrieval")
				}
				w.WriteHeader(test.code)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			r, err := New(server.Client()).Poll(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "secret"}, native.Task{UpstreamID: "id"})
			if (err != nil) != test.fails {
				t.Fatalf("unexpected error %v", err)
			}
			if !test.fails && r.Status != test.status {
				t.Fatalf("unexpected result %+v", r)
			}
			if r.Status == "failed" && r.Error != "blocked" {
				t.Fatal("missing failure reason")
			}
		})
	}
}

func TestFileRetrievalFailureIsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/query/video_generation" {
			_, _ = io.WriteString(w, `{"status":"Success","file_id":"file","base_resp":{"status_code":0}}`)
			return
		}
		_, _ = io.WriteString(w, `{"base_resp":{"status_code":1004}}`)
	}))
	defer server.Close()
	if _, err := New(server.Client()).Poll(context.Background(), gateway.Target{BaseURL: server.URL}, native.Task{UpstreamID: "id"}); err == nil {
		t.Fatal("missing download URL ignored")
	}
}

func TestDefaultsAndInvalidInput(t *testing.T) {
	for _, model := range []string{"MiniMax-Hailuo-02", "T2V-01"} {
		data, err := requestBody([]byte(`{"prompt":"hello","metadata":"{\"duration\":10}"}`), model)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Duration   int    `json:"duration"`
			Resolution string `json:"resolution"`
		}
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		resolution := "720P"
		if model == "MiniMax-Hailuo-02" {
			resolution = "768P"
		}
		if body.Duration != 10 || body.Resolution != resolution {
			t.Fatalf("defaults %s", data)
		}
	}
	for _, body := range []string{`null`, `{`, `{"metadata":[]}`, `{"duration":"invalid"}`, `{"size":123}`} {
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
		{`{"base_resp":{"status_code":1004,"status_msg":"auth error"}}`, 200, true},
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
