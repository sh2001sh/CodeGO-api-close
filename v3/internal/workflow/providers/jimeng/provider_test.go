package jimeng

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestSignedImageSubmitAndPollUseSameMappedKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/" || r.URL.Query().Get("Version") != "2022-08-31" {
			t.Errorf("invalid endpoint %s", r.URL.String())
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		payload := sha256.Sum256(body)
		if r.Header.Get("X-Content-Sha256") != hex.EncodeToString(payload[:]) || !strings.HasPrefix(r.Header.Get("Authorization"), "HMAC-SHA256 Credential=fixture-access/") || r.Header.Get("X-Date") == "" {
			t.Error("invalid signing headers")
		}
		var fields map[string]any
		if err := json.Unmarshal(body, &fields); err != nil {
			t.Error(err)
		}
		if fields["req_key"] != "jimeng_i2v_first_tail_v30_1080" {
			t.Errorf("wrong model %v", fields)
		}
		switch r.URL.Query().Get("Action") {
		case "CVSync2AsyncSubmitTask":
			if fields["frames"] != float64(241) || fields["prompt"] != "hello" || fields["seed"] != float64(42) {
				t.Errorf("wrong submission %v", fields)
			}
			if urls, ok := fields["image_urls"].([]any); !ok || len(urls) != 2 {
				t.Errorf("missing images %v", fields)
			}
			_, _ = w.Write([]byte(`{"code":10000,"data":{"task_id":"up-task"},"request_id":"submit-request"}`))
		case "CVSync2AsyncGetResult":
			if fields["task_id"] != "up-task" {
				t.Errorf("wrong task %v", fields)
			}
			_, _ = w.Write([]byte(`{"code":10000,"data":{"status":"done","video_url":"https://video.example/one","duration":9.5},"request_id":"poll-request"}`))
		default:
			t.Error("unknown action")
		}
	}))
	defer server.Close()
	target := gateway.Target{BaseURL: server.URL, Secret: "fixture-access|fixture-secret", UpstreamModel: "jimeng_v30_1080p"}
	p := New(server.Client())
	r, err := p.Submit(context.Background(), target, native.Submit{Model: "client-model", Body: []byte(`{"prompt":"hello","duration":10,"images":["https://image.example/first","https://image.example/tail"],"metadata":{"req_key":"spoof","seed":42}}`)})
	if err != nil || r.ID != "up-task" || r.Status != "queued" {
		t.Fatalf("submit %+v %v", r, err)
	}
	r, err = p.Poll(context.Background(), target, native.Task{UpstreamID: r.ID, Model: "client-model", Data: r.Data})
	if err != nil || r.Status != "completed" || r.URL != "https://video.example/one" || r.Units != 9.5 {
		t.Fatalf("poll %+v %v", r, err)
	}
	if !strings.Contains(string(r.Data), `"request_id":"poll-request"`) || !strings.Contains(string(r.Data), `"_v3_req_key":"jimeng_i2v_first_tail_v30_1080"`) {
		t.Error("poll metadata was lost")
	}
}

func TestMultipartRelayPreservesImagesAndFallbackSeconds(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("prompt", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("seconds", "5"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("metadata", `{"seed":3}`); err != nil {
		t.Fatal(err)
	}
	file, err := writer.CreateFormFile("input_reference", "first.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("fixture-image"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jimeng/" || r.Header.Get("Authorization") != "Bearer sk-fixture" || r.Header.Get("X-Date") != "" {
			t.Error("relay request signed or incorrect")
		}
		if r.URL.Query().Get("Action") == "CVSync2AsyncSubmitTask" {
			var fields map[string]any
			if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
				t.Error(err)
			}
			if fields["req_key"] != "jimeng_i2v_first_v30" || fields["frames"] != float64(121) || fields["seed"] != float64(3) {
				t.Errorf("payload %v", fields)
			}
			if images, ok := fields["binary_data_base64"].([]any); !ok || len(images) != 1 || images[0] != "Zml4dHVyZS1pbWFnZQ==" {
				t.Errorf("images %v", fields)
			}
			_, _ = w.Write([]byte(`{"code":10000,"data":{"task_id":"relay-task"}}`))
		} else {
			_, _ = w.Write([]byte(`{"code":10000,"data":{"status":"done","video_url":"https://video.example/result"}}`))
		}
	}))
	defer server.Close()
	target := gateway.Target{BaseURL: server.URL, Secret: "sk-fixture", UpstreamModel: "jimeng_v30"}
	p := New(server.Client())
	r, err := p.Submit(context.Background(), target, native.Submit{Body: body.Bytes(), ContentType: writer.FormDataContentType()})
	if err != nil || r.ID != "relay-task" {
		t.Fatalf("submit %+v %v", r, err)
	}
	r, err = p.Poll(context.Background(), target, native.Task{UpstreamID: r.ID, Data: r.Data})
	if err != nil || r.Status != "completed" || r.Units != 5 {
		t.Fatalf("poll %+v %v", r, err)
	}
}

func TestValidationAndProviderFailures(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"code":50400,"message":"unsupported model"}`))
	}))
	defer server.Close()
	p := New(server.Client())
	target := gateway.Target{BaseURL: server.URL, Secret: "invalid", UpstreamModel: "jimeng_v30"}
	for _, body := range []string{`null`, `{"duration":3}`, `{"images":["http://image.example/one","binary"]}`, `{"binary_data_base64":["invalid base64!"]}`, `{}`} {
		_, err := p.Submit(context.Background(), target, native.Submit{Body: []byte(body)})
		var invalid *native.InvalidRequest
		if !errors.As(err, &invalid) {
			t.Fatalf("expected local validation, got %v", err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid request reached provider")
	}
	target.Secret = "sk-fixture"
	r, err := p.Submit(context.Background(), target, native.Submit{Body: []byte(`{"prompt":"hello"}`)})
	if err != nil || r.Status != "failed" || r.Error != "unsupported model" {
		t.Fatalf("rejection %+v %v", r, err)
	}
	if _, err = p.Poll(context.Background(), target, native.Task{UpstreamID: "accepted-task"}); err == nil {
		t.Fatal("API-level polling rejection must preserve existing accepted task")
	}
}

func TestOversizedMultipartFileIsRejectedBeforeSubmission(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("input_reference", "too-large.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(bytes.Repeat([]byte("x"), maxImageBytes+1)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	_, err = New(server.Client()).Submit(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "sk-fixture", UpstreamModel: "jimeng_v30"}, native.Submit{Body: body.Bytes(), ContentType: writer.FormDataContentType()})
	var invalid *native.InvalidRequest
	if !errors.As(err, &invalid) || calls != 0 {
		t.Fatalf("oversized file accepted: calls %d, error %v", calls, err)
	}
}

func TestPollStatusesAndMalformedAcceptance(t *testing.T) {
	for _, tc := range []struct {
		name, body, status string
		wantErr            bool
	}{
		{"queued", `{"code":10000,"data":{"status":"in_queue"}}`, "queued", false},
		{"processing", `{"code":10000,"data":{"status":"generating"}}`, "in_progress", false},
		{"failed", `{"code":10000,"message":"generation failed","data":{"status":"failed"}}`, "failed", false},
		{"unknown", `{"code":10000,"data":{"status":"new-status"}}`, "", true},
		{"missing-code", `{"data":{"status":"done"}}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer server.Close()
			r, err := New(server.Client()).Poll(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "sk-fixture", UpstreamModel: "jimeng_v30"}, native.Task{UpstreamID: "id"})
			if (err != nil) != tc.wantErr || r.Status != tc.status {
				t.Fatalf("poll %+v %v", r, err)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"code":10000,"data":{}}`)) }))
	defer server.Close()
	_, err := New(server.Client()).Submit(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "sk-fixture", UpstreamModel: "jimeng_v30"}, native.Submit{Body: []byte(`{}`)})
	var invalid *native.InvalidRequest
	if err == nil || errors.As(err, &invalid) {
		t.Fatalf("missing ID must remain uncertain acceptance: %v", err)
	}
}
