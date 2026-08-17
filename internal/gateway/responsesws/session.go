package responsesws

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
)

const responsesWebSocketBeta = "responses_websockets=2026-02-06"

var ErrResponseInFlight = errors.New("responses websocket already has an in-flight response")

type Session struct {
	mu              sync.Mutex
	conn            *websocket.Conn
	channelID       int
	keyIndex        int
	bound           bool
	nativeSupported bool
	nativeDisabled  bool
	inFlight        bool
	dialer          *websocket.Dialer
}

func NewSession() *Session {
	return &Session{dialer: &websocket.Dialer{
		HandshakeTimeout:  20 * time.Second,
		EnableCompression: true,
		Proxy:             http.ProxyFromEnvironment,
	}}
}

func (s *Session) BindRoute(channelID, keyIndex int, nativeSupported bool) error {
	if channelID <= 0 || keyIndex < 0 {
		return errors.New("invalid responses websocket route")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bound && (s.channelID != channelID || s.keyIndex != keyIndex) {
		return errors.New("responses websocket route is already pinned")
	}
	s.channelID = channelID
	s.keyIndex = keyIndex
	s.bound = true
	s.nativeSupported = nativeSupported
	return nil
}

func (s *Session) Route() (channelID, keyIndex int, bound bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.channelID, s.keyIndex, s.bound
}

func (s *Session) NativeEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bound && s.nativeSupported && !s.nativeDisabled
}

func (s *Session) DisableNative() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nativeDisabled = true
	s.closeConnectionLocked()
}

func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil
	}
	err := s.conn.Close()
	s.conn = nil
	s.inFlight = false
	return err
}

func (s *Session) Do(ctx context.Context, endpoint string, headers http.Header, requestBody io.Reader) (*http.Response, error) {
	payload, err := nativeRequestPayload(requestBody)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if !s.bound || !s.nativeSupported || s.nativeDisabled {
		s.mu.Unlock()
		return nil, errors.New("native responses websocket is unavailable")
	}
	if s.inFlight {
		s.mu.Unlock()
		return nil, ErrResponseInFlight
	}
	conn, err := s.connectionLocked(ctx, endpoint, headers)
	if err != nil {
		s.nativeDisabled = true
		s.mu.Unlock()
		return nil, err
	}
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		s.nativeDisabled = true
		s.closeConnectionLocked()
		s.mu.Unlock()
		return nil, err
	}
	s.inFlight = true
	s.mu.Unlock()

	reader, writer := io.Pipe()
	go s.forwardResponse(ctx, conn, writer)
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       reader,
	}, nil
}

func (s *Session) connectionLocked(ctx context.Context, endpoint string, headers http.Header) (*websocket.Conn, error) {
	if s.conn != nil {
		return s.conn, nil
	}
	wsURL, err := websocketEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	headers = headers.Clone()
	headers.Set("OpenAI-Beta", responsesWebSocketBeta)
	conn, response, err := s.dialer.DialContext(ctx, wsURL, headers)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	s.conn = conn
	return conn, nil
}

func (s *Session) forwardResponse(ctx context.Context, conn *websocket.Conn, writer *io.PipeWriter) {
	defer func() {
		s.mu.Lock()
		s.inFlight = false
		s.mu.Unlock()
	}()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
	}
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			s.mu.Lock()
			s.nativeDisabled = true
			s.closeConnectionLocked()
			s.mu.Unlock()
			_ = writer.CloseWithError(err)
			return
		}
		if _, err := writer.Write(ssePayload(payload)); err != nil {
			s.mu.Lock()
			s.closeConnectionLocked()
			s.mu.Unlock()
			_ = writer.CloseWithError(err)
			return
		}
		if terminalWebSocketEvent(payload) {
			_ = writer.Close()
			return
		}
	}
}

func (s *Session) closeConnectionLocked() {
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

func nativeRequestPayload(reader io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, 32<<20))
	if err != nil {
		return nil, err
	}
	var request map[string]any
	if err := platformencoding.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	delete(request, "stream")
	delete(request, "background")
	request["type"] = "response.create"
	return platformencoding.Marshal(request)
}

func websocketEndpoint(endpoint string) (string, error) {
	switch {
	case strings.HasPrefix(endpoint, "https://"):
		return "wss://" + strings.TrimPrefix(endpoint, "https://"), nil
	case strings.HasPrefix(endpoint, "http://"):
		return "ws://" + strings.TrimPrefix(endpoint, "http://"), nil
	default:
		return "", fmt.Errorf("unsupported responses websocket endpoint: %s", endpoint)
	}
}

func ssePayload(payload []byte) []byte {
	return bytes.Join([][]byte{[]byte("data: "), payload, []byte("\n\n")}, nil)
}

func terminalWebSocketEvent(payload []byte) bool {
	var event struct {
		Type string `json:"type"`
	}
	if platformencoding.Unmarshal(payload, &event) != nil {
		return false
	}
	switch event.Type {
	case "response.completed", "response.failed", "response.incomplete", "error":
		return true
	default:
		return false
	}
}
