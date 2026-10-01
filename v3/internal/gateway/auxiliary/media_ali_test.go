package auxiliary

import (
	"bytes"
	"context"
	"errors"
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

func TestAliMediaPollingSuccessAndActualUsage(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/tasks/task-id" || r.Header.Get("Authorization") != "Bearer native-secret" {
			t.Errorf("poll request %s %s", r.Method, r.URL.Path)
		}
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"output":{"task_status":"RUNNING"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"output":{"task_status":"SUCCEEDED","results":[{"url":"https://example.com/a"},{"b64_image":"YmI="}]},"usage":{"input_tokens":6,"output_tokens":8,"image_count":2}}`))
	}))
	defer server.Close()
	adapter := &mediaAdapter{provider: "ali", pollInterval: time.Millisecond}
	req := &gateway.Request{Model: "wanx-v1", Body: []byte(`{"model":"wanx-v1"}`)}
	response, err := adapter.Decode(context.Background(), req, gateway.Target{BaseURL: server.URL, Secret: "native-secret"}, Input{Operation: Images}, mediaTestResponse(`{"output":{"task_id":"task-id"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || response.Header.Get("X-Codego-Image-Count") != "2" || response.Usage == nil || response.Usage.PromptTokens != 6 || response.Usage.CompletionTokens != 8 || response.Usage.Estimated {
		t.Fatalf("calls %d response %+v", calls.Load(), response)
	}
}

func TestAliMediaPollingCancelStopsInflightRequest(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(canceled) }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := (&mediaAdapter{provider: "ali"}).pollAli(ctx, gateway.Target{BaseURL: server.URL, Secret: "native-secret"}, "task-id")
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("poll did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("poll ignored cancellation")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("upstream request remained active")
	}
}

func TestAliMediaPollingFailuresDoNotReturnPartialImages(t *testing.T) {
	for _, status := range []string{"FAILED", "CANCELED", "UNKNOWN", ""} {
		t.Run(status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"output":{"task_status":"` + status + `","message":"native-secret","results":[{"url":"https://example.com/partial"}]}}`))
			}))
			defer server.Close()
			_, err := (&mediaAdapter{provider: "ali"}).pollAli(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "native-secret"}, "task-id")
			if err == nil || strings.Contains(err.Error(), "native-secret") {
				t.Fatalf("unsafe task failure %v", err)
			}
		})
	}
	for _, id := range []string{"", "../task", "a?b", "%2e%2e", ".."} {
		_, err := (&mediaAdapter{provider: "ali"}).pollAli(context.Background(), gateway.Target{BaseURL: "https://example.com"}, id)
		if err == nil {
			t.Errorf("invalid task ID accepted: %q", id)
		}
	}
}

func mediaEditInput(t *testing.T) Input {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, field := range []string{"image[]", "image[1]"} {
		part, err := writer.CreateFormFile(field, "photo.png")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})
	}
	_ = writer.Close()
	return Input{Operation: ImageEdits, ContentType: writer.FormDataContentType(), Body: body.Bytes()}
}

func TestNativeMultipartImageEditsConvertFilesAndMapModel(t *testing.T) {
	in := mediaEditInput(t)
	for _, tc := range []struct {
		provider, model, path, field string
		async                        bool
	}{
		{"ali", "qwen-image-edit", "/api/v1/services/aigc/multimodal-generation/generation", "input.messages.0.content", false},
		{"ali", "wanx2.1-imageedit", "/api/v1/services/aigc/image2image/image-synthesis", "input.images", true},
		{"ali", "wan2.6-image", "/api/v1/services/aigc/image-generation/generation", "input.messages.0.content", true},
		{"volcengine", "ep-seedream", "/api/v3/images/generations", "image", false},
	} {
		t.Run(tc.provider+"/"+tc.model, func(t *testing.T) {
			req := &gateway.Request{Model: "alias", Body: []byte(`{"model":"alias","prompt":"repair","n":2,"image[]_bytes":8}`)}
			request, err := mediaAdapters()[tc.provider].Build(context.Background(), req, gateway.Target{BaseURL: "https://example.com", Secret: "native-secret", UpstreamModel: tc.model}, in)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(request.Body)
			if request.URL.Path != tc.path || request.Header.Get("Content-Type") != "application/json" || gjson.GetBytes(data, "model").String() != tc.model {
				t.Fatalf("request %s %s", request.URL, data)
			}
			array := gjson.GetBytes(data, tc.field).Array()
			if strings.Contains(tc.field, "content") {
				if len(array) != 3 || array[0].Get("image").String() != "data:image/png;base64,iVBORw0KGgo=" || array[2].Get("text").String() != "repair" {
					t.Fatalf("multipart content %s", data)
				}
			} else if len(array) != 2 || array[0].String() != "data:image/png;base64,iVBORw0KGgo=" {
				t.Fatalf("multipart images %s", data)
			}
			if tc.async && request.Header.Get("X-DashScope-Async") != "enable" {
				t.Error("missing async header")
			}
			if tc.provider == "volcengine" && bytes.Contains(data, []byte("_bytes")) {
				t.Error("billing metadata exposed upstream")
			}
		})
	}
}

func TestNativeMultipartMissingImageFailsBeforeForwarding(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("prompt", "repair")
	_ = writer.Close()
	in := Input{Operation: ImageEdits, ContentType: writer.FormDataContentType(), Body: body.Bytes()}
	_, err := mediaAdapters()["ali"].Build(context.Background(), &gateway.Request{Model: "qwen-image-edit", Body: []byte(`{"prompt":"repair"}`)}, gateway.Target{}, in)
	if err == nil {
		t.Fatal("missing image accepted")
	}
}

func TestAliMediaPollingNegativeUsageFailsBeforeOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"task_status":"SUCCEEDED","results":[{"url":"https://example.com/result"}]},"usage":{"input_tokens":-1,"output_tokens":5}}`))
	}))
	defer server.Close()
	_, err := mediaAdapters()["ali"].Decode(context.Background(), &gateway.Request{Model: "wanx-v1"}, gateway.Target{BaseURL: server.URL}, Input{Operation: Images}, mediaTestResponse(`{"output":{"task_id":"job1"}}`))
	if err == nil {
		t.Fatal("negative asynchronous usage delivered")
	}
}
