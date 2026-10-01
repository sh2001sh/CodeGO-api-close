package credentials

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

func TestMarketRequestCancellationClosesOriginTLSAndProxyCONNECT(t *testing.T) {
	for _, throughProxy := range []bool{false, true} {
		t.Run(map[bool]string{false: "TLS", true: "CONNECT"}[throughProxy], func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = listener.Close() }()
			ready, done := make(chan struct{}), make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer func() { _ = conn.Close() }()
				reader := bufio.NewReader(conn)
				if throughProxy {
					connect, err := http.ReadRequest(reader)
					if err != nil || connect.Method != http.MethodConnect || connect.Host != "8.8.8.8:443" {
						t.Errorf("CONNECT request not pinned: %v %v", connect, err)
						return
					}
				} else if _, err = reader.Peek(1); err != nil {
					return
				}
				close(ready)
				_, _ = io.Copy(io.Discard, reader) // withhold CONNECT/TLS responses
			}()
			cfg := TransportConfig{DialTimeout: 5 * time.Second, TLSHandshakeTimeout: 5 * time.Second}
			var client *http.Client
			if throughProxy {
				pool := NewTransportPool(cfg)
				defer pool.CloseIdle()
				selected, _, err := pool.Client(1, "http://"+listener.Addr().String(), Fingerprint{})
				if err != nil {
					t.Fatal(err)
				}
				client, err = httpx.MarketClient(selected)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				dial := func(ctx context.Context, network, address string) (net.Conn, error) {
					if address != "8.8.8.8:443" {
						t.Errorf("origin not pinned: %s", address)
					}
					return (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
				}
				transport, err := newMarketTransport("8.8.8.8", netip.MustParseAddr("8.8.8.8"), cfg, Fingerprint{}, dial)
				if err != nil {
					t.Fatal(err)
				}
				defer transport.CloseIdleConnections()
				client = &http.Client{Transport: transport}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://8.8.8.8/v1/messages", strings.NewReader("native prompt"))
			req.Header.Set("Authorization", "Bearer native-key")
			result := make(chan error, 1)
			go func() { _, err := client.Do(req); result <- err }()
			select {
			case <-ready:
			case err := <-result:
				t.Fatalf("request failed before cancellation: %v", err)
			case <-time.After(2 * time.Second):
				t.Fatal("request never reached local fixture")
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("native request ignored cancellation")
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("cancelled native request leaked its origin/proxy connection")
			}
		})
	}
}
