package auxiliary

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/websocket"
)

func TestVolcMediaSelectedRejectsInvalidUpgradeAndClosesDuplex(t *testing.T) {
	for _, failure := range []string{"challenge", "status", "nonduplex"} {
		t.Run(failure, func(t *testing.T) {
			serverDone := make(chan struct{})
			server := httptest.NewServer(websocket.Handler(func(conn *websocket.Conn) {
				defer func() { _ = conn.Close(); close(serverDone) }()
				var frame []byte
				_ = websocket.Message.Receive(conn, &frame)
			}))
			defer server.Close()
			closed := make(chan struct{})
			calls := 0
			selected := &http.Client{Transport: vectorRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				response, err := server.Client().Transport.RoundTrip(r)
				if err != nil {
					return nil, err
				}
				body := &blockingMediaDuplex{ReadWriteCloser: response.Body.(io.ReadWriteCloser), closed: closed}
				response.Body = body
				switch failure {
				case "challenge":
					response.Header.Set("Sec-WebSocket-Accept", "invalid-challenge")
				case "status":
					response.StatusCode = http.StatusForbidden
				case "nonduplex":
					response.Body = mediaReadCloserOnly{body}
				}
				return response, nil
			})}
			ctx := context.WithValue(context.Background(), clientKey{}, selected)
			r, _ := localVolcMediaRequest(t, ctx, server.URL)
			response, err := mediaWebsocketRoundTrip(ctx, r)
			if err == nil || response != nil || calls != 1 {
				t.Fatalf("invalid %s accepted or retried: response=%v err=%v calls=%d", failure, response, err, calls)
			}
			waitMediaSignal(t, closed, "invalid upgrade close")
			waitMediaSignal(t, serverDone, "invalid upgrade disconnect")
		})
	}
}

type mediaReadCloserOnly struct{ io.ReadCloser }
