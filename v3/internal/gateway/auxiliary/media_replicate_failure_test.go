package auxiliary

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestMediaReplicateUploadFailureRefundsReservation(t *testing.T) {
	var creates atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/files" {
			creates.Add(1)
			t.Errorf("prediction attempted after upload failure: %s", r.URL.Path)
		}
		w.WriteHeader(500)
		_, _ = io.WriteString(w, "private-native-secret")
	}))
	defer server.Close()
	h, plan, settle, limits := testHandler(t, server.URL)
	plan.targets[0].Provider, plan.targets[0].UpstreamModel = "replicate", "owner/model"
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", "alias")
	_ = writer.WriteField("prompt", "repair")
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
	if w.Code != 502 || creates.Load() != 0 || !settle.finalized || settle.reserves != 1 || settle.out.Charge || settle.out.Delivered || limits.released != 1 || bytes.Contains(w.Body.Bytes(), []byte("private-native-secret")) {
		t.Fatalf("code=%d body=%s creates=%d settle=%+v leases=%d", w.Code, w.Body.String(), creates.Load(), settle, limits.released)
	}
}

func TestMediaReplicateInvalidChannelFailsOverWithoutSubmitting(t *testing.T) {
	var creates atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		creates.Add(1)
		_, _ = io.WriteString(w, `{"id":"job1","status":"succeeded","output":"https://example.com/result.png"}`)
	}))
	defer server.Close()
	h, plan, settle, limits := testHandler(t, server.URL, server.URL)
	for i := range plan.targets {
		plan.targets[i].Provider, plan.targets[i].UpstreamModel = "replicate", "owner/model"
	}
	plan.targets[0].Secret = ""
	w := invoke(h, "/v1/images/generations", `{"model":"alias","prompt":"paint"}`)
	if w.Code != 200 || creates.Load() != 1 || !settle.out.Charge || len(settle.req.Attempts) != 2 || limits.released != 2 {
		t.Fatalf("code=%d body=%s creates=%d outcome=%+v attempts=%d leases=%d", w.Code, w.Body.String(), creates.Load(), settle.out, len(settle.req.Attempts), limits.released)
	}
}
