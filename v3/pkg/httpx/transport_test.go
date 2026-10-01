package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPoolReusesTransportPerProxy(t *testing.T) {
	p := NewPool(TransportConfig{})
	a, err := p.Transport("")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := p.Transport("")
	c, err := p.Transport("http://127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("direct transport not reused")
	}
	if a == c {
		t.Fatal("proxy transport must be separate")
	}
	if _, err := p.Transport("ftp://x"); err == nil {
		t.Fatal("expected unsupported scheme error")
	}
}

func TestHeaderTimeoutCancelsSlowUpstream(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	p := NewPool(TransportConfig{})
	tr, _ := p.Transport("")
	ctx, _, cancel := WithHeaderTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	_, err := (&http.Client{Transport: tr}).Do(req)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(context.Cause(ctx), ErrHeaderTimeout) {
		t.Fatalf("cause = %v; want ErrHeaderTimeout", context.Cause(ctx))
	}
}

func TestHeaderTimeoutStopKeepsBodyStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(120 * time.Millisecond) // body continues past the header timeout
		_, _ = w.Write([]byte("done"))
	}))
	defer srv.Close()

	p := NewPool(TransportConfig{})
	tr, _ := p.Transport("")
	ctx, stop, cancel := WithHeaderTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	stop()
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "done" {
		t.Fatalf("body = %q, err=%v; want done", body, err)
	}
}
