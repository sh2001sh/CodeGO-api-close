package responsesws

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionReusesConnectionAndNormalizesRequests(t *testing.T) {
	var connectionCount atomic.Int32
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		connectionCount.Add(1)
		for turn := 0; turn < 2; turn++ {
			_, payload, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}
			var event map[string]any
			assert.NoError(t, platformencoding.Unmarshal(payload, &event))
			assert.Equal(t, "response.create", event["type"])
			assert.NotContains(t, event, "stream")
			assert.NotContains(t, event, "background")
			responseID := "resp_" + string(rune('1'+turn))
			assert.NoError(t, conn.WriteJSON(map[string]any{
				"type": "response.created", "response": map[string]any{"id": responseID},
			}))
			assert.NoError(t, conn.WriteJSON(map[string]any{
				"type": "response.completed", "response": map[string]any{"id": responseID, "output": []any{}},
			}))
		}
	}))
	defer server.Close()

	session := NewSession()
	require.NoError(t, session.BindRoute(7, 0, true))
	defer session.Close()
	for turn := 0; turn < 2; turn++ {
		response, err := session.Do(context.Background(), server.URL+"/v1/responses", http.Header{}, strings.NewReader(`{"model":"gpt-5","stream":true,"background":false}`))
		require.NoError(t, err)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), "response.completed")
	}
	require.EqualValues(t, 1, connectionCount.Load())
}

func TestSessionDoesNotReplayAfterCreateFrameWasWritten(t *testing.T) {
	var connectionCount atomic.Int32
	var requestCount atomic.Int32
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		connectionCount.Add(1)
		_, _, readErr := conn.ReadMessage()
		if readErr != nil {
			return
		}
		requestCount.Add(1)
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1013, "try again later"), time.Now().Add(time.Second))
	}))
	defer server.Close()

	session := NewSession()
	require.NoError(t, session.BindRoute(8, 0, true))
	response, err := session.Do(context.Background(), server.URL+"/v1/responses", http.Header{}, strings.NewReader(`{"model":"gpt-5","stream":true}`))
	require.NoError(t, err)
	_, err = io.ReadAll(response.Body)
	require.Error(t, err)
	require.EqualValues(t, 1, connectionCount.Load())
	require.EqualValues(t, 1, requestCount.Load())
	require.True(t, session.NativeEnabled())
}

func TestSessionDoesNotReplayAfterFirstUpstreamEvent(t *testing.T) {
	var connectionCount atomic.Int32
	var requestCount atomic.Int32
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		connectionCount.Add(1)
		_, _, readErr := conn.ReadMessage()
		if readErr != nil {
			return
		}
		requestCount.Add(1)
		_ = conn.WriteJSON(map[string]any{
			"type": "response.created", "response": map[string]any{"id": "resp_partial"},
		})
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1013, "try again later"), time.Now().Add(time.Second))
	}))
	defer server.Close()

	session := NewSession()
	require.NoError(t, session.BindRoute(10, 0, true))
	response, err := session.Do(context.Background(), server.URL+"/v1/responses", http.Header{}, strings.NewReader(`{"model":"gpt-5","stream":true}`))
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.Error(t, err)
	require.Contains(t, string(body), "response.created")
	require.EqualValues(t, 1, connectionCount.Load())
	require.EqualValues(t, 1, requestCount.Load())
	require.True(t, session.NativeEnabled())
}

func TestSessionRejectsReplayAfterPartialClose(t *testing.T) {
	var connectionCount atomic.Int32
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		connection := connectionCount.Add(1)
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		_ = conn.WriteJSON(map[string]any{
			"type": "response.created", "response": map[string]any{"id": "resp_turn"},
		})
		if connection == 1 {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1013, "try again later"), time.Now().Add(time.Second))
			return
		}
		_ = conn.WriteJSON(map[string]any{
			"type": "response.completed", "response": map[string]any{"id": "resp_turn", "output": []any{}},
		})
	}))
	defer server.Close()

	session := NewSession()
	require.NoError(t, session.BindRoute(12, 0, true))
	first, err := session.Do(context.Background(), server.URL+"/v1/responses", http.Header{}, strings.NewReader(`{"model":"gpt-5"}`))
	require.NoError(t, err)
	_, err = io.ReadAll(first.Body)
	require.Error(t, err)
	_, err = session.Do(context.Background(), server.URL+"/v1/responses", http.Header{}, strings.NewReader(`{"model":"gpt-5"}`))
	require.ErrorIs(t, err, ErrResponseInFlight)
	require.EqualValues(t, 1, connectionCount.Load())
	require.True(t, session.ReplayForbidden())
	require.True(t, session.NativeEnabled())
}

func TestSessionDisablesNativeAfterAuthenticationHandshakeFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	session := NewSession()
	require.NoError(t, session.BindRoute(11, 0, true))
	_, err := session.Do(context.Background(), server.URL+"/v1/responses", http.Header{}, strings.NewReader(`{"model":"gpt-5","stream":true}`))
	require.Error(t, err)
	require.False(t, session.NativeEnabled())
}

func TestSessionAllowsOnlyOneInFlightResponse(t *testing.T) {
	release := make(chan struct{})
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
		<-release
	}))
	defer server.Close()

	session := NewSession()
	require.NoError(t, session.BindRoute(9, 0, true))
	first, err := session.Do(context.Background(), server.URL+"/v1/responses", http.Header{}, strings.NewReader(`{"model":"gpt-5"}`))
	require.NoError(t, err)
	_, err = session.Do(context.Background(), server.URL+"/v1/responses", http.Header{}, strings.NewReader(`{"model":"gpt-5"}`))
	require.ErrorIs(t, err, ErrResponseInFlight)
	close(release)
	_ = first.Body.Close()
	_ = session.Close()
}
