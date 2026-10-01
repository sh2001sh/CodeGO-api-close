package ali

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestSubmitPollContent(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/services/aigc/video-generation/video-synthesis":
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer fixture-key" || r.Header.Get("X-DashScope-Async") != "enable" {
				t.Errorf("bad submit method or auth")
			}
			var body struct {
				Model      string
				Input      map[string]any
				Parameters map[string]any
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Model != "wan2.5-i2v-preview" || body.Input["img_url"] != "https://images.example/a.png" || body.Parameters["duration"] != float64(6) || body.Parameters["resolution"] != "720P" {
				t.Errorf("unexpected submit body %+v", body)
			}
			_, _ = io.WriteString(w, `{"output":{"task_id":"ali-task","task_status":"PENDING"}}`)
		case "/api/v1/tasks/ali-task":
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer fixture-key" {
				t.Error("bad poll auth")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"output": map[string]any{"task_id": "ali-task", "task_status": "SUCCEEDED", "video_url": server.URL + "/media"}, "usage": map[string]any{"duration": "6", "video_count": 2}})
		case "/media":
			if r.Header.Get("Authorization") != "" {
				t.Error("credential leaked to public media")
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = io.WriteString(w, "video-bytes")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	p := New(server.Client())
	target := gateway.Target{BaseURL: server.URL, Secret: "fixture-key", UpstreamModel: "wan2.5-i2v-preview"}
	result, err := p.Submit(context.Background(), target, native.Submit{Model: "alias", Body: []byte(`{"prompt":"waves","input_reference":"https://images.example/a.png","seconds":"6","size":"720p"}`)})
	if err != nil || result.ID != "ali-task" || result.Status != "queued" || result.Units != 0 {
		t.Fatalf("submit %+v %v", result, err)
	}
	task := native.Task{UpstreamID: result.ID, Data: result.Data}
	result, err = p.Poll(context.Background(), target, task)
	if err != nil || result.Status != "completed" || result.Units != 12 {
		t.Fatalf("poll %+v %v", result, err)
	}
	task.Data = result.Data
	resp, err := p.Content(context.Background(), target, task)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	data, _ := io.ReadAll(resp.Body)
	if string(data) != "video-bytes" {
		t.Errorf("content %q", data)
	}
}

func TestSubmissionFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name, body                string
		status                    int
		invalid, rejected, failed bool
	}{
		{"preflight", `{"prompt":"x","duration":-1}`, 200, true, false, false},
		{"rejected", `{"prompt":"x"}`, 429, false, true, false},
		{"application", `{"prompt":"x"}`, 200, false, false, true},
		{"missing-id", `{"prompt":"x"}`, 201, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.invalid {
					t.Error("invalid input reached upstream")
				}
				w.WriteHeader(tc.status)
				if tc.failed {
					_, _ = io.WriteString(w, `{"code":"InvalidParameter","message":"invalid size"}`)
				} else {
					_, _ = io.WriteString(w, `{}`)
				}
			}))
			defer server.Close()
			r, err := New(server.Client()).Submit(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "key"}, native.Submit{Model: "wan2.5-t2v-preview", Body: []byte(tc.body)})
			var invalid *native.InvalidRequest
			var rejected *native.Rejected
			if errors.As(err, &invalid) != tc.invalid || errors.As(err, &rejected) != tc.rejected {
				t.Fatalf("wrong error classification %v", err)
			}
			if tc.failed {
				if err != nil || r.Status != "failed" || r.Error != "invalid size" {
					t.Fatalf("application error %+v %v", r, err)
				}
			} else if err == nil {
				t.Fatal("failure returned nil error")
			}
		})
	}
}

func TestNativeAndMultipartBody(t *testing.T) {
	var data bytes.Buffer
	writer := multipart.NewWriter(&data)
	for k, v := range map[string]string{"prompt": "cat", "seconds": "7", "size": "1280x720", "metadata": `{"input":{"negative_prompt":"blur"},"parameters":{"seed":42}}`} {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := payload(native.Submit{Model: "alias", Body: data.Bytes(), ContentType: writer.FormDataContentType()}, "wan2.5-t2v-preview")
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	_ = json.Unmarshal(body, &fields)
	params := fields["parameters"].(map[string]any)
	if params["duration"] != float64(7) || params["seed"] != float64(42) || params["size"] != "1280*720" {
		t.Fatalf("body %s", body)
	}
	body, err = payload(native.Submit{Model: "alias", Body: []byte(`{"model":"other","input":{"prompt":"cat"},"parameters":{"duration":5}}`)}, "mapped")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(body, &fields)
	if fields["model"] != "mapped" {
		t.Errorf("mapping overridden %s", body)
	}
}
