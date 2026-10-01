package gemini

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

func TestSubmitPollAndContentPreservesOperationPath(t *testing.T) {
	const operation = "models/veo-3.1-generate-preview/operations/task-id"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Goog-Api-Key") != "fixture-key" || r.Header.Get("Authorization") != "" {
			t.Error("incorrect Gemini authentication")
		}
		switch r.URL.Path {
		case "/v1beta/models/veo-3.1-generate-preview:predictLongRunning":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			params := body["parameters"].(map[string]any)
			if params["durationSeconds"] != float64(6) || params["resolution"] != "1080p" || params["aspectRatio"] != "9:16" || params["sampleCount"] != float64(1) {
				t.Errorf("params %+v", params)
			}
			if _, exists := body["model"]; exists {
				t.Error("model unexpectedly in native body")
			}
			_, _ = io.WriteString(w, `{"name":"`+operation+`"}`)
		case "/v1beta/" + operation:
			if r.Method != http.MethodGet || r.URL.RawPath != "" {
				t.Error("operation slash escaped")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"name": operation, "done": true, "response": map[string]any{"generateVideoResponse": map[string]any{"generatedVideos": []any{map[string]any{"video": map[string]any{"uri": server.URL + "/file", "durationSeconds": 6}}}}}})
		case "/file":
			_, _ = io.WriteString(w, "movie")
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	p := New(server.Client())
	target := gateway.Target{BaseURL: server.URL + "/v1beta", Secret: "fixture-key", UpstreamModel: "veo-3.1-generate-preview"}
	r, err := p.Submit(context.Background(), target, native.Submit{Model: "alias", Body: []byte(`{"prompt":"cat","size":"1080x1920","duration":6}`)})
	if err != nil || r.ID != operation || r.Status != "queued" {
		t.Fatalf("submit %+v %v", r, err)
	}
	task := native.Task{UpstreamID: r.ID, Data: r.Data}
	r, err = p.Poll(context.Background(), target, task)
	if err != nil || r.Status != "completed" || r.Units != 6 {
		t.Fatalf("poll %+v %v", r, err)
	}
	task.Data = r.Data
	resp, err := p.Content(context.Background(), target, task)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "movie" {
		t.Errorf("content %q", body)
	}
}

func TestInvalidSubmitAndOperation(t *testing.T) {
	p := New(nil)
	_, err := p.Submit(context.Background(), gateway.Target{Secret: "key"}, native.Submit{Model: "veo", Body: []byte(`{"prompt":"cat","images":["https://unsafe.example/image"]}`)})
	var invalid *native.InvalidRequest
	if !errors.As(err, &invalid) {
		t.Fatalf("invalid image error %v", err)
	}
	for _, id := range []string{"models/veo/operations/../secret", "models/veo/operations/id?key=x", "https://evil.example/operations/id", "models/veo/operations/"} {
		if ValidOperation(id) {
			t.Errorf("accepted invalid operation %q", id)
		}
		if _, err := p.Poll(context.Background(), gateway.Target{}, native.Task{UpstreamID: id}); err == nil {
			t.Errorf("polled invalid ID %q", id)
		}
	}
}

func TestSubmitRejectsHTTPButKeepsMalformedAcceptanceAmbiguous(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		rejected   bool
	}{
		{"rejected", `{"error":{"message":"rate limited"}}`, 429, true},
		{"missing-operation", `{}`, 200, false},
		{"invalid-json", `not-json`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			_, err := New(server.Client()).Submit(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "fixture-key"}, native.Submit{Model: "veo", Body: []byte(`{"prompt":"cat"}`)})
			var rejected *native.Rejected
			var invalid *native.InvalidRequest
			if err == nil || errors.As(err, &rejected) != tc.rejected || errors.As(err, &invalid) {
				t.Fatalf("incorrect classification: %v", err)
			}
		})
	}
}

func TestMultipartImageConvertedAndMetadata(t *testing.T) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	_ = writer.WriteField("prompt", "cat")
	_ = writer.WriteField("seconds", "8")
	_ = writer.WriteField("metadata", `{"resolution":"1080P","generateAudio":false}`)
	part, err := writer.CreateFormFile("input_reference", "image.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("\x89PNG\r\n\x1a\nimage"))
	_ = writer.Close()
	body, err := Payload(native.Submit{Body: buffer.Bytes(), ContentType: writer.FormDataContentType()})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	_ = json.Unmarshal(body, &fields)
	image := fields["instances"].([]any)[0].(map[string]any)["image"].(map[string]any)
	params := fields["parameters"].(map[string]any)
	if image["mimeType"] != "image/png" || image["bytesBase64Encoded"] != "iVBORw0KGgppbWFnZQ==" || params["durationSeconds"] != float64(8) || params["generateAudio"] != false {
		t.Fatalf("payload %s", body)
	}
}

func TestOperationFailureAndMissingMedia(t *testing.T) {
	for _, tc := range []struct {
		body, status string
		wantErr      bool
		units        float64
	}{
		{`{"name":"operations/id","done":true,"error":{"code":8,"message":"exhausted"}}`, "failed", false, 0},
		{`{"name":"operations/id","done":true,"response":{"raiMediaFilteredCount":1}}`, "failed", false, 0},
		{`{"name":"operations/id","done":false}`, "in_progress", false, 0},
		{`{"name":"operations/id","done":true}`, "", true, 0},
		{`{"name":"operations/id","done":true,"response":{"videos":[{"bytesBase64Encoded":"dmlkZW8=","mimeType":"video/mp4"}]}}`, "completed", false, 0},
	} {
		r, err := ParseResult([]byte(tc.body), "")
		if (err != nil) != tc.wantErr || r.Status != tc.status || r.Units != tc.units {
			t.Errorf("body %s result %+v err %v", tc.body, r, err)
		}
	}
}

func TestContentRejectsExternalPrivateHostAndRedirect(t *testing.T) {
	reached := false
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		if r.Header.Get("X-Goog-Api-Key") != "" {
			t.Error("key leaked")
		}
		_, _ = io.WriteString(w, "media")
	}))
	defer external.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, external.URL, http.StatusFound) }))
	defer source.Close()
	target := gateway.Target{BaseURL: source.URL, Secret: "fixture-key"}
	_, err := Content(context.Background(), source.Client(), target, external.URL)
	if err == nil || reached {
		t.Fatalf("unsafe external media fetched: %v", err)
	}
	_, err = Content(context.Background(), source.Client(), target, source.URL+"/redirect")
	var rejection *native.Rejected
	if !errors.As(err, &rejection) || reached {
		t.Fatalf("redirect followed or not rejected: %v", err)
	}
}
