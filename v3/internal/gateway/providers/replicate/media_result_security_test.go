package replicate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestReplicateBase64ResultCannotReadDifferentPrivateOrigin(t *testing.T) {
	var reads atomic.Int32
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reads.Add(1)
		_, _ = w.Write([]byte("PRIVATE_INTERNAL_BYTES"))
	}))
	defer private.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"status":"succeeded","output":%q}`, private.URL+"/private")
	}))
	defer provider.Close()
	stream := run(t, context.Background(), Provider{}, imageRequest(`{"prompt":"hello","response_format":"b64_json"}`), target(provider.URL))
	defer func() { _ = stream.Close() }()
	event, err := stream.Next()
	if err != nil || event.Kind != gateway.EventError || event.Err.Code != "image_download_failed" || reads.Load() != 0 {
		t.Fatalf("private result was accepted: event=%+v err=%v reads=%d", event, err, reads.Load())
	}
}
