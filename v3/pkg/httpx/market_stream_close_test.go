package httpx

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const marketStreamDone = "data: [DONE]\n\n"

func marketStreamClient(t *testing.T, handler http.HandlerFunc) (*http.Client, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	origin := httptest.NewTLSServer(handler)
	t.Cleanup(origin.Close)
	var dials, lookups atomic.Int64
	base := origin.Client().Transport.(*http.Transport).Clone()
	base.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		dials.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, origin.Listener.Addr().String())
	}
	pool := NewPool(TransportConfig{})
	t.Cleanup(pool.CloseIdle)
	client, err := pool.MarketClient(123, &http.Client{Transport: base})
	if err != nil {
		t.Fatal(err)
	}
	client.Transport.(*marketTransport).lookup = func(ctx context.Context, network, host string) ([]netip.Addr, error) {
		lookups.Add(1)
		return imageLookup(ctx, network, host)
	}
	return client, &dials, &lookups
}

func readMarketStreamDone(t *testing.T, response *http.Response) {
	t.Helper()
	if response.ProtoMajor != 1 || len(response.TransferEncoding) != 1 || response.TransferEncoding[0] != "chunked" {
		t.Fatalf("fixture must exercise TLS HTTP/1.1 chunked EOF: %s %v", response.Proto, response.TransferEncoding)
	}
	data := make([]byte, len(marketStreamDone))
	if _, err := io.ReadFull(response.Body, data); err != nil || string(data) != marketStreamDone {
		t.Fatalf("provider completion marker: %q %v", data, err)
	}
}

func TestMarketStreamCloseAfterDoneReusesTLSConnection(t *testing.T) {
	client, dials, lookups := marketStreamClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Trailer", "X-Stream-Finished")
		_, _ = io.WriteString(w, marketStreamDone)
		w.(http.Flusher).Flush()
		// [DONE] is an application terminal event; the final HTTP chunk and
		// trailer arrive afterward, as they do through a streaming proxy.
		select {
		case <-time.After(5 * time.Millisecond):
			_, _ = io.WriteString(w, ": trailing keepalive\n\n")
			w.Header().Set("X-Stream-Finished", "yes")
		case <-r.Context().Done():
		}
	})
	trailers := 0
	for i := 0; i < 20; i++ {
		response, err := client.Post("https://example.com/v1/chat/completions", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		readMarketStreamDone(t, response)
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatal("second close:", err)
		}
		if response.Trailer.Get("X-Stream-Finished") == "yes" {
			trailers++
		}
	}
	if dials.Load() != 1 || lookups.Load() != 20 {
		t.Fatalf("20 provider terminal closes must reuse one TLS connection while validating DNS each request: dials=%d lookups=%d", dials.Load(), lookups.Load())
	}
	if trailers != 20 {
		t.Fatalf("close did not consume final HTTP trailers: %d/20", trailers)
	}
	t.Logf("20 SSE terminal closes: %d TLS connection, %d DNS policy checks", dials.Load(), lookups.Load())
}

func TestMarketStreamCloseBoundsStalledTailAndDoesNotCancelCaller(t *testing.T) {
	client, dials, _ := marketStreamClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, marketStreamDone)
		w.(http.Flusher).Flush()
		if r.URL.Path == "/stall" {
			<-r.Context().Done()
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/stall", nil)
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	readMarketStreamDone(t, response)
	start := time.Now()
	errorsSeen := make(chan error, 12)
	var callers sync.WaitGroup
	for i := 0; i < cap(errorsSeen); i++ {
		callers.Add(1)
		go func() { defer callers.Done(); errorsSeen <- response.Body.Close() }()
	}
	callers.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if !errors.Is(err, ErrMarketBodyDrainTimeout) {
			t.Fatalf("stalled cleanup must report the timeout to every closer: %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("stalled body close exceeded bounded cleanup: %s", elapsed)
	}
	if ctx.Err() != nil {
		t.Fatal("cleanup canceled the caller's context")
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/healthy", nil)
	response, err = client.Do(req)
	if err != nil {
		t.Fatal("caller cannot make a subsequent request:", err)
	}
	readMarketStreamDone(t, response)
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if dials.Load() != 2 {
		t.Fatalf("stalled connection must be discarded, then a healthy connection opened: %d", dials.Load())
	}
	t.Logf("stalled tail canceled in %s; caller remains live", time.Since(start))
}

func TestMarketStreamCloseRejectsOversizedTailWithoutReusingConnection(t *testing.T) {
	client, dials, _ := marketStreamClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, marketStreamDone)
		w.(http.Flusher).Flush()
		if r.URL.Path == "/oversized" {
			_, _ = io.WriteString(w, strings.Repeat("x", marketBodyDrainBytes*4))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}
	})
	response, err := client.Post("https://example.com/oversized", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	readMarketStreamDone(t, response)
	start := time.Now()
	if err := response.Body.Close(); !errors.Is(err, ErrMarketBodyDrainLimit) {
		t.Fatalf("oversized cleanup must report its byte limit: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("oversized close exceeded bounded cleanup: %s", elapsed)
	}
	response, err = client.Post("https://example.com/healthy", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	readMarketStreamDone(t, response)
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if dials.Load() != 2 {
		t.Fatalf("oversized tail connection was reused: %d TLS dials", dials.Load())
	}
}

func TestMarketStreamCallerCancellationSkipsTailCleanup(t *testing.T) {
	client, _, _ := marketStreamClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, marketStreamDone)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/stall", nil)
	response, err := client.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	readMarketStreamDone(t, response)
	cancel()
	start := time.Now()
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > marketBodyDrainTimeout/2 {
		t.Fatalf("canceled request still waited for a body tail: %s", elapsed)
	}
}

func TestMarketStreamCloseUnblocksConcurrentRead(t *testing.T) {
	client, _, _ := marketStreamClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, marketStreamDone)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	response, err := client.Post("https://example.com/stall", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	readMarketStreamDone(t, response)
	started, readResult := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		_, err := response.Body.Read(make([]byte, 1))
		readResult <- err
	}()
	<-started
	if err := response.Body.Close(); err != nil && !errors.Is(err, ErrMarketBodyDrainTimeout) {
		t.Fatal(err)
	}
	select {
	case err := <-readResult:
		if err == nil {
			t.Fatal("concurrent Read unexpectedly received data")
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not unblock concurrent body Read")
	}
}

func TestMarketUpgradeCloseSkipsTailCleanup(t *testing.T) {
	client, _, _ := marketStreamClient(t, func(w http.ResponseWriter, _ *http.Request) {
		conn, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = buffered.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = buffered.Flush()
		_, _ = buffered.ReadByte()
	})
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/realtime", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatal("fixture did not upgrade")
	}
	start := time.Now()
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > marketBodyDrainTimeout/2 {
		t.Fatalf("upgraded body was drained on close: %s", elapsed)
	}
}

func TestMarketImageCloseRetainsConservativeNoDrainPolicy(t *testing.T) {
	client, _, _ := marketStreamClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, "partial image")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	image, err := client.Transport.(*marketTransport).PublicImageTransport("example.com", netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{Transport: image}).Get("https://example.com/image")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > marketBodyDrainTimeout/2 {
		t.Fatalf("untrusted image body was drained on close: %s", elapsed)
	}
}
