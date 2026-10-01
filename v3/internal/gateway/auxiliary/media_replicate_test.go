package auxiliary

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestMediaReplicateDefaultRegistryCreatesOncePollsAndDownloads(t *testing.T) {
	var creates, polls, downloads atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models/owner/model/predictions":
			creates.Add(1)
			if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer native-secret" || r.Header.Get("Prefer") != "wait" {
				t.Error("native create auth/method")
			}
			data, _ := io.ReadAll(r.Body)
			if gjson.GetBytes(data, "input.prompt").String() != "paint" || gjson.GetBytes(data, "input.num_outputs").Int() != 2 || gjson.GetBytes(data, "input.aspect_ratio").String() != "1:1" {
				t.Errorf("native input %s", data)
			}
			w.WriteHeader(201)
			_, _ = io.WriteString(w, `{"id":"job1","status":"processing","urls":{"get":"`+server.URL+`/v1/predictions/job1"}}`)
		case "/v1/predictions/job1":
			polls.Add(1)
			if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer native-secret" {
				t.Error("native poll auth/method")
			}
			_, _ = io.WriteString(w, `{"id":"job1","status":"succeeded","output":["`+server.URL+`/media/a","`+server.URL+`/media/b"]}`)
		case "/media/a", "/media/b":
			downloads.Add(1)
			if r.Header.Get("Authorization") != "" {
				t.Error("prediction key sent to image download")
			}
			_, _ = w.Write([]byte{0, 1, 2, 255})
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	h, plan, settle, limits := testHandler(t, server.URL+"/v1")
	h.cfg.RelayTimeout = 5 * time.Second
	plan.targets[0].Provider, plan.targets[0].Secret, plan.targets[0].UpstreamModel = "replicate", "native-secret", "owner/model"
	w := invoke(h, "/v1/images/generations", `{"model":"alias","prompt":"paint","n":2,"size":"1024x1024","response_format":"b64_json"}`)
	if w.Code != 200 || creates.Load() != 1 || polls.Load() != 1 || downloads.Load() != 2 {
		t.Fatalf("code=%d body=%s creates=%d polls=%d downloads=%d", w.Code, w.Body.String(), creates.Load(), polls.Load(), downloads.Load())
	}
	if gjson.Get(w.Body.String(), "data.0.b64_json").String() != "AAEC/w==" || len(gjson.Get(w.Body.String(), "data").Array()) != 2 {
		t.Fatalf("image output %s", w.Body.String())
	}
	if !settle.out.Charge || !settle.out.Delivered || !settle.out.Usage.Estimated || settle.out.Usage.ImageCount != 2 || !settle.finalized || settle.reserves != 1 || limits.released != 1 {
		t.Fatalf("settle %+v limits %+v", settle, limits)
	}
}

func TestMediaReplicateMultipartUploadPrecedesPrediction(t *testing.T) {
	var uploads, creates atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer configured-secret" {
			t.Error("native authentication missing")
		}
		switch r.URL.Path {
		case "/v1/files":
			uploads.Add(1)
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				return
			}
			file, header, err := r.FormFile("content")
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = file.Close() }()
			data, _ := io.ReadAll(file)
			if header.Filename != "photo.png" || !bytes.Equal(data, []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
				t.Errorf("uploaded file %s %v", header.Filename, data)
			}
			w.WriteHeader(201)
			_, _ = io.WriteString(w, `{"urls":{"get":"`+server.URL+`/uploads/image1"}}`)
		case "/v1/models/owner/model/predictions":
			creates.Add(1)
			data, _ := io.ReadAll(r.Body)
			if uploads.Load() != 1 || gjson.GetBytes(data, "input.image_prompt").String() != server.URL+"/uploads/image1" || bytes.Contains(data, []byte("_bytes")) {
				t.Errorf("edit prediction %s uploads=%d", data, uploads.Load())
			}
			_, _ = io.WriteString(w, `{"id":"edit1","status":"succeeded","output":["https://example.com/result.png"]}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	h, plan, settle, _ := testHandler(t, server.URL)
	plan.targets[0].Provider, plan.targets[0].Secret, plan.targets[0].UpstreamModel = "replicate", "native-secret", "owner/model"
	plan.targets[0].HeaderOverride = map[string]string{"Authorization": "Bearer configured-secret"}
	var in Input
	var body bytes.Buffer
	// Include the ordinary client fields in the same original multipart body.
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", "alias")
	_ = writer.WriteField("prompt", "repair")
	part, err := writer.CreateFormFile("image[]", "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})
	_ = writer.Close()
	in.Body, in.ContentType = body.Bytes(), writer.FormDataContentType()
	r := httptest.NewRequest("POST", "/v1/images/edits", bytes.NewReader(in.Body))
	r.Header.Set("Authorization", "Bearer client-key")
	r.Header.Set("Content-Type", in.ContentType)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || uploads.Load() != 1 || creates.Load() != 1 || !settle.out.Charge {
		t.Fatalf("code=%d body=%s upload=%d create=%d outcome=%+v", w.Code, w.Body.String(), uploads.Load(), creates.Load(), settle.out)
	}
}

func TestMediaReplicateValidationAndUploadFailureRefund(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"missing image", `{"model":"alias","prompt":"repair"}`, 400},
		{"invalid count", `{"model":"alias","prompt":"paint","n":17}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			defer server.Close()
			h, plan, settle, _ := testHandler(t, server.URL)
			plan.targets[0].Provider, plan.targets[0].UpstreamModel = "replicate", "owner/model"
			path := "/v1/images/generations"
			if tc.name == "missing image" {
				path = "/v1/images/edits"
			}
			w := invoke(h, path, tc.body)
			if w.Code != tc.status || calls.Load() != 0 || !settle.finalized || settle.out.Charge {
				t.Fatalf("code=%d calls=%d outcome=%+v", w.Code, calls.Load(), settle.out)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, "native-secret")
	}))
	defer server.Close()
	_, err := uploadReplicateMedia(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "native-secret"}, mediaEditInput(t), nil)
	if err == nil || strings.Contains(err.Error(), "native-secret") {
		t.Fatalf("unsafe upload failure %v", err)
	}
}

func TestMediaReplicateUploadCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := mediaEditInput(t)
	finished := make(chan error, 1)
	go func() {
		_, err := uploadReplicateMedia(ctx, gateway.Target{BaseURL: server.URL, Secret: "native-secret"}, in, nil)
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upload did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if err != context.Canceled {
			t.Fatalf("upload cancel %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("upload ignored cancellation")
	}
}
