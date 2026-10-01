package xunfei

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// RoundTrip performs Spark's native WebSocket exchange. The gateway must
// dispatch this optional transport instead of its HTTP client for this provider.
// Its response body exposes each native JSON message as one bounded SSE event;
// Decode converts those messages for both streaming and collected Chat clients.
func (Provider) RoundTrip(req *http.Request) (*http.Response, error) {
	return nativeRoundTrip(req, directConnection)
}

type connectionFactory func(*http.Request, *websocket.Config) (*websocket.Conn, io.Closer, error)

func directConnection(req *http.Request, config *websocket.Config) (*websocket.Conn, io.Closer, error) {
	handshake, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	conn, err := config.DialContext(handshake)
	return conn, nil, err
}

func nativeRoundTrip(req *http.Request, connect connectionFactory) (*http.Response, error) {
	if req == nil || req.URL == nil || req.Body == nil {
		return nil, errors.New("xunfei: invalid transport request")
	}
	defer func() { _ = req.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(req.Body, maxFrame+1))
	if err != nil || len(body) > maxFrame || !json.Valid(body) {
		return nil, errors.New("xunfei: invalid native request body")
	}
	u := *req.URL
	origin := u.Scheme + "://" + u.Host
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return nil, errors.New("xunfei: invalid transport scheme")
	}
	config, err := websocket.NewConfig(u.String(), origin)
	if err != nil {
		return nil, errors.New("xunfei: invalid WebSocket configuration")
	}
	conn, transport, err := connect(req, config)
	if err != nil {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		// websocket.DialError includes the signed URL. Never return that error.
		return nil, errors.New("xunfei: WebSocket handshake failed")
	}
	conn.MaxPayloadBytes = maxFrame
	reader := &socketBody{conn: conn, transport: transport, ctx: req.Context()}
	reader.stop = context.AfterFunc(req.Context(), reader.closeTransport)
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	writeTimer := time.AfterFunc(5*time.Second, reader.closeTransport)
	defer writeTimer.Stop()
	if err := websocket.Message.Send(conn, string(body)); err != nil {
		_ = reader.Close()
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		return nil, errors.New("xunfei: WebSocket request write failed")
	}
	_ = conn.SetWriteDeadline(time.Time{})
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader, Request: req}, nil
}

type socketBody struct {
	conn      *websocket.Conn
	transport io.Closer
	ctx       context.Context
	buffer    []byte
	stop      func() bool
	once      sync.Once
	closeErr  error
}

func (b *socketBody) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	if b.ctx.Err() != nil {
		return 0, b.ctx.Err()
	}
	if len(b.buffer) == 0 {
		var message []byte
		if err := websocket.Message.Receive(b.conn, &message); err != nil {
			if b.ctx.Err() != nil {
				return 0, b.ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				return 0, io.EOF
			}
			return 0, errors.New("xunfei: WebSocket response read failed")
		}
		// Compact JSON prevents line breaks from creating extra SSE fields.
		var compact bytes.Buffer
		if err := json.Compact(&compact, message); err != nil {
			return 0, errors.New("xunfei: malformed native response")
		}
		b.buffer = append([]byte("data: "), compact.Bytes()...)
		b.buffer = append(b.buffer, '\n', '\n')
	}
	n := copy(dst, b.buffer)
	b.buffer = b.buffer[n:]
	return n, nil
}

func (b *socketBody) Close() error {
	if b.stop != nil {
		b.stop()
	}
	b.closeTransport()
	return b.closeErr
}

func (b *socketBody) closeTransport() {
	b.once.Do(func() {
		// An HTTP upgrade body exposes no network deadlines. Close it first so
		// pending frame writes cannot block the WebSocket control-frame close.
		if b.transport != nil {
			_ = b.transport.Close()
		}
		// Close writes a WS control frame; bound it before closing the socket.
		_ = b.conn.SetWriteDeadline(time.Now().Add(time.Second))
		b.closeErr = b.conn.Close()
	})
}
