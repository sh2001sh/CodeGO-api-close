package auxiliary

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestVolcMediaSelectedHandshakeCancellationStopsTransport(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _ := localVolcMediaRequest(t, ctx, server.URL)
	finished := make(chan error, 1)
	go func() {
		_, err := mediaWebsocketRoundTrip(context.WithValue(ctx, clientKey{}, server.Client()), r)
		finished <- err
	}()
	waitMediaSignal(t, started, "handshake start")
	cancel()
	waitMediaError(t, finished, context.Canceled)
	waitMediaSignal(t, stopped, "handshake cancellation")
}

func TestVolcMediaSelectedCancellationClosesStalledDuplex(t *testing.T) {
	for _, tc := range []struct {
		mode     string
		deadline bool
	}{{"write", false}, {"read", false}, {"read", true}} {
		name := tc.mode
		if tc.deadline {
			name += "-deadline"
		}
		t.Run(name, func(t *testing.T) {
			serverDone := make(chan struct{})
			server := httptest.NewServer(websocket.Handler(func(conn *websocket.Conn) {
				defer func() { _ = conn.Close(); close(serverDone) }()
				var frame []byte
				if websocket.Message.Receive(conn, &frame) == nil {
					_ = websocket.Message.Receive(conn, &frame)
				}
			}))
			defer server.Close()
			entered, closed := make(chan struct{}), make(chan struct{})
			selected := &http.Client{Transport: vectorRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				response, err := server.Client().Transport.RoundTrip(r)
				if err == nil {
					response.Body = &blockingMediaDuplex{ReadWriteCloser: response.Body.(io.ReadWriteCloser), mode: tc.mode, entered: entered, closed: closed}
				}
				return response, err
			})}
			ctx, cancel := context.WithCancel(context.Background())
			if tc.deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 250*time.Millisecond)
			}
			defer cancel()
			r, _ := localVolcMediaRequest(t, ctx, server.URL)
			finished := make(chan error, 1)
			go func() {
				_, err := mediaWebsocketRoundTrip(context.WithValue(ctx, clientKey{}, selected), r)
				finished <- err
			}()
			waitMediaSignal(t, entered, "stalled "+tc.mode)
			want := context.Canceled
			if tc.deadline {
				want = context.DeadlineExceeded
			} else {
				cancel()
			}
			waitMediaError(t, finished, want)
			waitMediaSignal(t, closed, "duplex close")
			waitMediaSignal(t, serverDone, "server disconnect")
		})
	}
}

type blockingMediaDuplex struct {
	io.ReadWriteCloser
	mode             string
	entered, closed  chan struct{}
	enter, closeOnce sync.Once
}

func (b *blockingMediaDuplex) Read(data []byte) (int, error) {
	if b.mode == "read" {
		b.enter.Do(func() { close(b.entered) })
		<-b.closed
		return 0, io.ErrClosedPipe
	}
	return b.ReadWriteCloser.Read(data)
}

func (b *blockingMediaDuplex) Write(data []byte) (int, error) {
	if b.mode == "write" {
		b.enter.Do(func() { close(b.entered) })
		<-b.closed
		return 0, io.ErrClosedPipe
	}
	return b.ReadWriteCloser.Write(data)
}

func (b *blockingMediaDuplex) Close() error {
	var err error
	b.closeOnce.Do(func() { close(b.closed); err = b.ReadWriteCloser.Close() })
	return err
}

func waitMediaSignal(t *testing.T, signal <-chan struct{}, operation string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(operation + " remained blocked")
	}
}

func waitMediaError(t *testing.T, finished <-chan error, want error) {
	t.Helper()
	select {
	case err := <-finished:
		if !errors.Is(err, want) {
			t.Fatalf("cancellation error %v, want %v", err, want)
		}
	case <-time.After(time.Second):
		t.Fatal("selected WebSocket cancellation remained blocked")
	}
}
