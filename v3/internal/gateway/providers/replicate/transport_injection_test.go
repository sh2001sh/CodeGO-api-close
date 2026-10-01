package replicate

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestInjectedTransportCoversCreatePollAndBase64Download(t *testing.T) {
	var creates, polls, downloads int
	const imageBytes = "test image bytes"
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := ""
		switch {
		case req.Method == http.MethodPost:
			creates++
			if req.Header.Get("Authorization") != "Bearer test-key" {
				t.Error("missing create key")
			}
			body = `{"id":"job1","status":"starting"}`
		case req.URL.Host == "api.example.invalid":
			polls++
			if req.Header.Get("Authorization") != "Bearer test-key" {
				t.Error("missing poll key")
			}
			body = `{"id":"job1","status":"succeeded","output":"https://cdn.example.invalid/output.png"}`
		case req.URL.Host == "cdn.example.invalid":
			downloads++
			if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
				t.Error("credential leaked to image CDN")
			}
			body = imageBytes
		default:
			t.Errorf("unexpected injected request: %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	p := Provider{PollInterval: time.Millisecond}.WithTransport(base)
	request := imageRequest(`{"prompt":"hello","response_format":"b64_json"}`)
	chosen := target("https://api.example.invalid")
	chosen.ProxyURL = "http://proxy.example.invalid" // The injected base already owns the channel proxy.
	out, err := BuildImageRequest(context.Background(), request, chosen)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.RoundTrip(out)
	if err != nil {
		t.Fatal(err)
	}
	stream := DecodeImages(request, resp)
	defer func() { _ = stream.Close() }()
	event, err := stream.Next()
	if err != nil || event.Kind != gateway.EventData || !strings.Contains(string(event.Payload), `"b64_json":"`+base64.StdEncoding.EncodeToString([]byte(imageBytes))+`"`) || event.Usage != nil {
		t.Fatalf("bad base64 image output: %#v %v", event, err)
	}
	if creates != 1 || polls != 1 || downloads != 1 {
		t.Fatalf("injected transport counts %d/%d/%d", creates, polls, downloads)
	}
}

func TestPollCannotChangePredictionIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = fmt.Fprint(w, `{"id":"job1","status":"starting"}`)
		} else {
			_, _ = fmt.Fprint(w, `{"id":"different-job","status":"succeeded","output":"https://images.example/1"}`)
		}
	}))
	defer server.Close()
	stream := run(t, context.Background(), Provider{PollInterval: time.Millisecond}, imageRequest(`{"prompt":"hello"}`), target(server.URL))
	defer func() { _ = stream.Close() }()
	event, err := stream.Next()
	if err != nil || event.Kind != gateway.EventError || event.Err.Code != "invalid_response" {
		t.Fatalf("accepted different prediction: %#v %v", event, err)
	}
}

func TestPollingStopsAtFailedAndCanceledTerminals(t *testing.T) {
	for _, status := range []string{"failed", "canceled"} {
		t.Run(status, func(t *testing.T) {
			var polls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					_, _ = fmt.Fprint(w, `{"id":"job1","status":"starting"}`)
				} else {
					polls.Add(1)
					_, _ = fmt.Fprintf(w, `{"id":"job1","status":%q,"error":"job stopped"}`, status)
				}
			}))
			defer server.Close()
			stream := run(t, context.Background(), Provider{PollInterval: time.Millisecond}, imageRequest(`{"prompt":"hello"}`), target(server.URL))
			defer func() { _ = stream.Close() }()
			event, err := stream.Next()
			if err != nil || event.Kind != gateway.EventError || event.Err.Code != "prediction_"+status || polls.Load() != 1 {
				t.Fatalf("terminal %s not preserved: %#v err=%v polls=%d", status, event, err, polls.Load())
			}
		})
	}
}

func TestCancelInFlightPollingRequest(t *testing.T) {
	pollStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = fmt.Fprint(w, `{"id":"job1","status":"starting"}`)
			return
		}
		close(pollStarted)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := run(t, ctx, Provider{PollInterval: time.Millisecond}, imageRequest(`{"prompt":"hello"}`), target(server.URL))
	defer func() { _ = stream.Close() }()
	result := make(chan error, 1)
	go func() { _, err := stream.Next(); result <- err }()
	select {
	case <-pollStarted:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("polling request never started")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("in-flight cancel lost: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("polling HTTP request ignored cancellation")
	}
}

func TestImageDownloadFailureDoesNotReturnSuccessfulImages(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusTemporaryRedirect} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "/other")
				w.WriteHeader(status)
			}))
			defer server.Close()
			body := fmt.Sprintf(`{"status":"succeeded","output":%q}`, server.URL+"/image.png")
			stream := DecodeImages(imageRequest(`{"prompt":"hello","response_format":"b64_json"}`), &http.Response{Body: io.NopCloser(strings.NewReader(body))})
			defer func() { _ = stream.Close() }()
			event, err := stream.Next()
			if err != nil || event.Kind != gateway.EventError || event.Err.Code != "image_download_failed" {
				t.Fatalf("failed download returned success: %#v %v", event, err)
			}
		})
	}
}
