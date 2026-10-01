package replicate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestUntrustedPollURLsNeverReceiveCredentials(t *testing.T) {
	var foreignCalls atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignCalls.Add(1)
		_, _ = fmt.Fprint(w, `{"id":"job1","status":"succeeded","output":"https://images.example/1"}`)
	}))
	defer foreign.Close()
	for _, pollURL := range []string{
		foreign.URL + "/v1/predictions/job1", "/v1/predictions/other", "/v1/predictions/../private", "/private/job1",
		"//user:pass@example.com/v1/predictions/job1", "/v1/predictions/job1?token=x", "/v1/predictions/job1#fragment",
	} {
		t.Run(pollURL, func(t *testing.T) {
			var polls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					_, _ = fmt.Fprintf(w, `{"id":"job1","status":"processing","urls":{"get":%q}}`, pollURL)
				} else {
					polls.Add(1)
					_, _ = fmt.Fprint(w, `{}`)
				}
			}))
			defer server.Close()
			stream := run(t, context.Background(), Provider{PollInterval: time.Millisecond}, imageRequest(`{"prompt":"hello"}`), target(server.URL))
			defer func() { _ = stream.Close() }()
			event, err := stream.Next()
			if err != nil || event.Kind != gateway.EventError || event.Err.Code != "invalid_response" {
				t.Fatalf("malicious poll URL accepted: %#v %v", event, err)
			}
			if polls.Load() != 0 {
				t.Fatal("credentials sent to invalid polling path")
			}
		})
	}
	if foreignCalls.Load() != 0 {
		t.Fatal("foreign origin received a polling request")
	}
}

func TestCreateAndPollRedirectsAreRejected(t *testing.T) {
	var leaks atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaks.Add(1) }))
	defer foreign.Close()
	for _, redirectOnCreate := range []bool{true, false} {
		t.Run(fmt.Sprint(redirectOnCreate), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if redirectOnCreate || r.Method == http.MethodGet {
					http.Redirect(w, r, foreign.URL, http.StatusTemporaryRedirect)
				} else {
					_, _ = fmt.Fprint(w, `{"id":"job1","status":"starting"}`)
				}
			}))
			defer server.Close()
			p := Provider{PollInterval: time.Millisecond}
			request := imageRequest(`{"prompt":"hello"}`)
			out, err := BuildImageRequest(context.Background(), request, target(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			// An outer client must not follow a redirect returned by the provider.
			resp, err := (&http.Client{Transport: p}).Do(out)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if redirectOnCreate {
				if resp.StatusCode != http.StatusBadGateway {
					t.Fatalf("create redirect was exposed: %d", resp.StatusCode)
				}
			} else {
				stream := DecodeImages(request, resp)
				defer func() { _ = stream.Close() }()
				event, err := stream.Next()
				if err != nil || event.Kind != gateway.EventError || event.Err.Code != "poll_failed" {
					t.Fatalf("poll redirect accepted: %#v %v", event, err)
				}
			}
		})
	}
	if leaks.Load() != 0 {
		t.Fatal("foreign origin received credential/body redirect")
	}
}

func TestCreateHTTPErrorRetainsStatusAndRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "15")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"detail":"rate limited"}`)
	}))
	defer server.Close()
	out, err := BuildImageRequest(context.Background(), imageRequest(`{"prompt":"hello"}`), target(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (Provider{}).RoundTrip(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 429 || resp.Header.Get("Retry-After") != "15" || string(data) != `{"detail":"rate limited"}` {
		t.Fatalf("HTTP failure lost information: status=%d headers=%v data=%s err=%v", resp.StatusCode, resp.Header, data, err)
	}
}

func TestPendingPredictionCancellationDeadlineAndClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"id":"job1","status":"starting"}`)
	}))
	defer server.Close()
	for _, mode := range []string{"cancel", "deadline", "close"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if mode == "deadline" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
			}
			defer cancel()
			stream := run(t, ctx, Provider{PollInterval: time.Hour}, imageRequest(`{"prompt":"hello"}`), target(server.URL))
			defer func() { _ = stream.Close() }()
			started, result := make(chan struct{}), make(chan error, 1)
			go func() { close(started); _, err := stream.Next(); result <- err }()
			<-started
			want := context.Canceled
			switch mode {
			case "cancel":
				cancel()
			case "close":
				_ = stream.Close()
			case "deadline":
				want = context.DeadlineExceeded
			}
			select {
			case err := <-result:
				if !errors.Is(err, want) {
					t.Fatalf("expected %v, got %v", want, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("prediction polling ignored cancellation")
			}
		})
	}
}

func TestCanceledCreateAndTruncatedNativeResponses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := BuildImageRequest(ctx, imageRequest(`{"prompt":"hello"}`), target("https://example.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (Provider{}).RoundTrip(out); !errors.Is(err, context.Canceled) {
		t.Fatalf("create lost cancellation: %v", err)
	}
	for _, body := range []string{"", `{"status":"succeeded","output":`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, body) }))
		stream := run(t, context.Background(), Provider{}, imageRequest(`{"prompt":"hello"}`), target(server.URL))
		event, err := stream.Next()
		_ = stream.Close()
		server.Close()
		if body == "" && !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("empty native response accepted: %#v %v", event, err)
		}
		if body != "" && (err != nil || event.Kind != gateway.EventError || event.Err.Code != "invalid_response") {
			t.Fatalf("truncated native response accepted: %#v %v", event, err)
		}
	}
}
