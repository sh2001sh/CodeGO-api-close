package credentials

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestImageTLSHandshakeHonorsParentCancellationAndConfiguredDeadline(t *testing.T) {
	for _, tc := range []struct {
		name              string
		parent, handshake time.Duration
	}{
		{"parent", 250 * time.Millisecond, 5 * time.Second},
		{"handshake", 5 * time.Second, 250 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = listener.Close() }()
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer func() { _ = conn.Close() }()
				_, _ = io.Copy(io.Discard, conn) // never send the expected TLS server hello
			}()
			dial := func(ctx context.Context, network, address string) (net.Conn, error) {
				if address != "8.8.8.8:443" {
					t.Errorf("handshake origin not pinned: %s", address)
				}
				return (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
			}
			transport, err := newImageTransport("example.com", netip.MustParseAddr("8.8.8.8"), TransportConfig{TLSHandshakeTimeout: tc.handshake}, Fingerprint{}, dial)
			if err != nil {
				t.Fatal(err)
			}
			defer transport.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), tc.parent)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/image", nil)
			if err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			_, err = transport.RoundTrip(req)
			if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second {
				t.Fatalf("handshake deadline ignored: error=%v elapsed=%v", err, time.Since(started))
			}
			_ = listener.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("cancelled handshake leaked its origin connection")
			}
		})
	}
}

func TestImagePinningUsesCanonicalIPv4AndIPv6Addresses(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "image.example" {
			t.Errorf("HTTP host changed to pinned IP: %s", r.Host)
		}
		_, _ = io.WriteString(w, "image")
	}))
	defer origin.Close()
	for _, tc := range []struct{ ip, want string }{{"::ffff:8.8.8.8", "8.8.8.8:80"}, {"2606:4700:4700::1111", "[2606:4700:4700::1111]:80"}} {
		dial := func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != tc.want {
				t.Errorf("pinned address=%s want=%s", address, tc.want)
			}
			return (&net.Dialer{}).DialContext(ctx, network, origin.Listener.Addr().String())
		}
		transport, err := newImageTransport("image.example", netip.MustParseAddr(tc.ip), TransportConfig{}, Fingerprint{}, dial)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := (&http.Client{Transport: transport}).Get("http://image.example/image")
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		transport.CloseIdleConnections()
		if err != nil {
			t.Fatal(err)
		}
	}
}
