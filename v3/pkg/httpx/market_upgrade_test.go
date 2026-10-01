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
	"sync/atomic"
	"testing"
	"time"
)

func TestMarketTransportRetainsDuplexUpgrade(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = buffered.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = buffered.Flush()
		line, readErr := buffered.ReadString('\n')
		if readErr == nil && line == "ping\n" {
			_, _ = buffered.WriteString("pong\n")
			_ = buffered.Flush()
		}
	}))
	defer origin.Close()
	standard := origin.Client().Transport.(*http.Transport).Clone()
	standard.TLSClientConfig.ServerName = ""
	standard.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, origin.Listener.Addr().String())
	}
	client, err := MarketClient(&http.Client{Transport: standard})
	if err != nil {
		t.Fatal(err)
	}
	client.Transport.(*marketTransport).lookup = imageLookup
	r, _ := http.NewRequest(http.MethodGet, "https://example.com/realtime", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	duplex, ok := response.Body.(io.ReadWriteCloser)
	if !ok || response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatal("market transport discarded the upgraded duplex connection")
	}
	if _, err = duplex.Write([]byte("ping\n")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 5)
	if _, err = io.ReadFull(duplex, got); err != nil || string(got) != "pong\n" {
		t.Fatalf("upgraded connection did not transmit both ways: %q %v", got, err)
	}
}

func TestMarketTransportRejectsForeignHostBeforeLookup(t *testing.T) {
	var calls atomic.Int64
	transport := &marketTransport{base: &http.Transport{}, lookup: func(ctx context.Context, network, host string) ([]netip.Addr, error) {
		calls.Add(1)
		return imageLookup(ctx, network, host)
	}}
	r := httptest.NewRequest(http.MethodPost, "https://example.com/v1/chat/completions", nil)
	r.Host = "private.example"
	if _, err := transport.RoundTrip(r); !errors.Is(err, ErrMarketTransportPolicy) || calls.Load() != 0 {
		t.Fatalf("foreign Host reached lookup: %v, calls %d", err, calls.Load())
	}
}

func TestMarketBodyRejectsWritesToOrdinaryResponses(t *testing.T) {
	body := &marketBody{ReadCloser: io.NopCloser(strings.NewReader("response")), cleanup: func() {}}
	writer, ok := any(body).(io.Writer)
	if !ok {
		t.Fatal("response wrapper must preserve its explicit write contract")
	}
	if _, err := writer.Write([]byte("data")); !errors.Is(err, ErrMarketTransportPolicy) {
		t.Fatalf("ordinary response write must fail explicitly: %v", err)
	}
}
