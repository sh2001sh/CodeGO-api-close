package openai_video

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

func TestSubmitJSONRemixAndBasePath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.EscapedPath() != "/proxy/v1/videos/source%2Fid/remix" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing authorization")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid JSON body")
		}
		if body["model"] != "sora-upstream" || body["prompt"] != "remix prompt" || body["custom"] != true {
			t.Errorf("unexpected body %#v", body)
		}
		_, _ = io.WriteString(w, `{"id":"video-id","status":"queued"}`)
	}))
	defer server.Close()
	input := native.Submit{Action: "remixGenerate", OriginID: "source/id", Body: []byte(`{"model":"client-model","prompt":"remix prompt","custom":true}`)}
	result, err := New(server.Client()).Submit(context.Background(), gateway.Target{BaseURL: server.URL + "/proxy/v1", Secret: "secret", UpstreamModel: "sora-upstream"}, input)
	if err != nil || result.ID != "video-id" || result.Status != "queued" {
		t.Fatalf("result %+v error %v", result, err)
	}
	if string(result.Data) != `{"id":"video-id","status":"queued"}` {
		t.Fatal("upstream response not preserved")
	}
}

func TestMultipartPreservesFilesAndReplacesAllModels(t *testing.T) {
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	if err := writer.WriteField("model", "first-client-model"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("model", "second-client-model"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("prompt", "a cat"); err != nil {
		t.Fatal(err)
	}
	file, err := writer.CreateFormFile("input_reference", "image.png")
	if err != nil {
		t.Fatal(err)
	}
	image := []byte{0x89, 'P', 'N', 'G', 0, 1, 2}
	if _, err := file.Write(image); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/videos" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("incorrect endpoint/auth")
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer func() { _ = r.MultipartForm.RemoveAll() }()
		if values := r.MultipartForm.Value["model"]; len(values) != 1 || values[0] != "mapped" {
			t.Errorf("models %v", values)
		}
		if r.FormValue("prompt") != "a cat" {
			t.Error("prompt changed")
		}
		f, header, err := r.FormFile("input_reference")
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = f.Close() }()
		data, _ := io.ReadAll(f)
		if !bytes.Equal(data, image) || header.Filename != "image.png" {
			t.Error("reference file changed")
		}
		_, _ = io.WriteString(w, `{"task_id":"task","status":"in_progress"}`)
	}))
	defer server.Close()
	r, err := New(server.Client()).Submit(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "secret", UpstreamModel: "mapped"}, native.Submit{Body: payload.Bytes(), ContentType: writer.FormDataContentType()})
	if err != nil || r.ID != "task" || r.Status != "in_progress" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestPollStatusesAndUsage(t *testing.T) {
	cases := []struct {
		response, status string
		units            float64
		fails            bool
	}{
		{`{"id":"id","status":"completed","seconds":"8"}`, "completed", 8, false},
		{`{"id":"id","status":"completed","seconds":4}`, "completed", 4, false},
		{`{"id":"id","status":"failed","error":{"message":"rejected"}}`, "failed", 0, false},
		{`{"id":"id","status":"cancelled"}`, "failed", 0, false},
		{`{"id":"id","status":"unexpected"}`, "", 0, true},
		{`{}`, "", 0, true},
		{`bad JSON`, "", 0, true},
	}
	for _, test := range cases {
		t.Run(test.response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/v1/videos/id" || r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("incorrect polling request")
				}
				_, _ = io.WriteString(w, test.response)
			}))
			defer server.Close()
			r, err := New(server.Client()).Poll(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "secret"}, native.Task{UpstreamID: "id"})
			if (err != nil) != test.fails {
				t.Fatalf("unexpected error %v", err)
			}
			if !test.fails && (r.Status != test.status || r.Units != test.units || string(r.Data) != test.response) {
				t.Fatalf("unexpected result %+v", r)
			}
		})
	}
}

func TestContentAndHTTPFailure(t *testing.T) {
	for _, code := range []int{200, 401} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/videos/id/content" || r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("incorrect content request")
				}
				w.Header().Set("Content-Type", "video/mp4")
				w.WriteHeader(code)
				_, _ = io.WriteString(w, "video bytes")
			}))
			defer server.Close()
			resp, err := New(server.Client()).Content(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "secret"}, native.Task{UpstreamID: "id"})
			if code == 401 {
				if err == nil || resp != nil {
					t.Fatal("provider error accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			data, _ := io.ReadAll(resp.Body)
			if string(data) != "video bytes" || resp.Header.Get("Content-Type") != "video/mp4" {
				t.Fatal("content changed")
			}
		})
	}
}

func TestInvalidInputAndCancellation(t *testing.T) {
	p := New(nil)
	target := gateway.Target{BaseURL: "https://example.invalid", UpstreamModel: "model"}
	for _, input := range []native.Submit{
		{Action: "remix", Body: []byte(`{}`)},
		{Body: []byte(`null`)},
		{Body: []byte(`{}`), ContentType: "text/plain"},
		{Body: []byte("--boundary\r\nContent-Disposition: form-data; name=prompt\r\n\r\ntruncated"), ContentType: "multipart/form-data; boundary=boundary"},
	} {
		if _, err := p.Submit(context.Background(), target, input); err == nil {
			t.Fatalf("invalid request accepted %+v", input)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Poll(ctx, target, native.Task{UpstreamID: "id"}); err == nil {
		t.Fatal("cancelled request succeeded")
	}
	if _, err := p.Poll(context.Background(), target, native.Task{}); err == nil {
		t.Fatal("empty upstream ID accepted")
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
