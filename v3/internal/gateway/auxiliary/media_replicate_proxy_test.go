package auxiliary

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tidwall/gjson"
)

func TestMediaReplicateUploadCreatePollDownloadShareSelectedProxy(t *testing.T) {
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/v1/files":
			if r.URL.Host != "provider.invalid" || r.Header.Get("Authorization") != "Bearer native-secret" {
				t.Error("upload bypassed channel/proxy auth")
			}
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(201)
			_, _ = io.WriteString(w, `{"urls":{"get":"http://provider.invalid/uploaded/image1"}}`)
		case "/v1/models/owner/model/predictions":
			if r.URL.Host != "provider.invalid" || r.Header.Get("Authorization") != "Bearer native-secret" {
				t.Error("prediction bypassed channel/proxy auth")
			}
			body, _ := io.ReadAll(r.Body)
			if gjson.GetBytes(body, "input.image_prompt").String() != "http://provider.invalid/uploaded/image1" {
				t.Errorf("uploaded image missing %s", body)
			}
			w.WriteHeader(202)
			_, _ = io.WriteString(w, `{"id":"job1","status":"processing","urls":{"get":"http://provider.invalid/v1/predictions/job1"}}`)
		case "/v1/predictions/job1":
			if r.Header.Get("Authorization") != "Bearer native-secret" {
				t.Error("poll lost native credential")
			}
			_, _ = io.WriteString(w, `{"id":"job1","status":"succeeded","output":"http://provider.invalid/image"}`)
		case "/image":
			if r.URL.Host != "provider.invalid" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Error("same-origin download bypassed proxy or leaked credential")
			}
			_, _ = w.Write([]byte{0, 1, 2})
		default:
			t.Errorf("unexpected proxied route %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer proxy.Close()
	h, plan, settle, _ := testHandler(t, "http://provider.invalid/v1")
	h.cfg.RelayTimeout = 5 * time.Second
	plan.targets[0].Provider, plan.targets[0].Secret, plan.targets[0].UpstreamModel, plan.targets[0].ProxyURL = "replicate", "native-secret", "owner/model", proxy.URL
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", "alias")
	_ = writer.WriteField("prompt", "repair")
	_ = writer.WriteField("response_format", "b64_json")
	part, err := writer.CreateFormFile("image", "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte{0, 1, 2})
	_ = writer.Close()
	r := httptest.NewRequest("POST", "/v1/images/edits", &body)
	r.Header.Set("Authorization", "Bearer client-key")
	r.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || requests.Load() != 4 || gjson.Get(w.Body.String(), "data.0.b64_json").String() != "AAEC" || !settle.out.Charge {
		t.Fatalf("code=%d requests=%d body=%s outcome=%+v", w.Code, requests.Load(), w.Body.String(), settle.out)
	}
}
