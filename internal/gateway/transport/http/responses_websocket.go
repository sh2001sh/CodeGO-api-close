package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sh2001sh/new-api/constant"
	responsesws "github.com/sh2001sh/new-api/internal/gateway/responsesws"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"
	"github.com/sh2001sh/new-api/types"
)

const responsesWebsocketMaxDuration = 60 * time.Minute

const responsesWebsocketRoutingContextKey = "responses_websocket_routing_context"

type responsesWebsocketRoutingContext struct {
	ChannelID int
	KeyIndex  int
	Routing   responsesBackgroundRoutingContext
}

var responsesWebsocketUpgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

// ResponsesWebsocket serves the persistent Responses API WebSocket transport.
func ResponsesWebsocket(c *gin.Context) {
	conn, err := responsesWebsocketUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	sessionCtx, cancelSession := context.WithTimeout(c.Request.Context(), responsesWebsocketMaxDuration)
	defer cancelSession()
	deadline, _ := sessionCtx.Deadline()
	_ = conn.SetReadDeadline(deadline)
	upstreamSession := responsesws.NewSession()
	defer upstreamSession.Close()
	serveResponsesWebsocket(conn, sessionCtx, cancelSession, func(ctx context.Context, writer *responsesWebsocketHTTPWriter, body []byte) {
		executeResponsesWebsocketTurn(c, writer, ctx, upstreamSession, body)
	})
}

// Keep reading control frames while the relay runs. Only one turn uses the
// pinned route and continuation cache at a time; data writes share one lock.
func serveResponsesWebsocket(conn *websocket.Conn, sessionCtx context.Context, cancelSession context.CancelFunc, execute func(context.Context, *responsesWebsocketHTTPWriter, []byte)) {
	state := &responsesWebsocketState{}
	var writeMu sync.Mutex
	writePayload := func(payload []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
		return conn.WriteMessage(websocket.TextMessage, payload)
	}
	writeError := func(status int, code, message, param string) {
		if writePayload(responsesWebsocketErrorPayload(status, code, message, param)) != nil {
			cancelSession()
		}
	}
	messages := make(chan []byte)
	readerDone := make(chan struct{})
	conn.SetReadLimit(32 << 20)
	go func() {
		defer close(readerDone)
		defer cancelSession()
		for {
			messageType, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
				continue
			}
			select {
			case messages <- payload:
			case <-sessionCtx.Done():
				return
			}
		}
	}()
	var pending [][]byte
	var pendingBytes int
	var turn *responsesWebsocketTurn
	var writer *responsesWebsocketHTTPWriter
	var finished chan struct{}
	var cancelTurn context.CancelFunc
	defer func() {
		cancelSession()
		_ = conn.Close()
		if cancelTurn != nil {
			cancelTurn()
			<-finished // Do not release the Gin context before billing unwinds.
		}
		<-readerDone
	}()

	for {
		var payload []byte
		if finished == nil && len(pending) > 0 {
			payload, pending = pending[0], pending[1:]
			pendingBytes -= len(payload)
		} else {
			select {
			case <-sessionCtx.Done():
				return
			case <-finished:
				cancelTurn()
				cancelTurn = nil
				finished = nil
				responseID, output, completed := writer.Completion()
				if writer.Interrupted() {
					if err := writer.SendCancelled(); err != nil {
						return
					}
				}
				if completed {
					state.complete(turn, responseID, output)
				} else {
					state.fail(turn)
				}
				continue
			case payload = <-messages:
			}
		}
		if sessionCtx.Err() != nil {
			return
		}
		var event struct {
			Type       string `json:"type"`
			ResponseID string `json:"response_id"`
		}
		if json.Unmarshal(payload, &event) == nil && (event.Type == "response.interrupt" || event.Type == "response.cancel") {
			if writer != nil && finished != nil {
				if !writer.Interrupt(event.ResponseID) {
					writeError(400, "response_not_found", "No active response matches response_id", "response_id")
				}
			} else if event.ResponseID != "" {
				writeError(400, "response_not_found", "No active response matches response_id", "response_id")
			}
			continue
		}
		if finished != nil {
			// Bound queued input without blocking control frames or retaining
			// an unbounded collection of large conversation bodies.
			if len(pending) >= 16 || pendingBytes+len(payload) > 32<<20 {
				writeError(429, "websocket_queue_full", "Too many queued websocket requests", "")
			} else {
				pending = append(pending, payload)
				pendingBytes += len(payload)
			}
			continue
		}
		var normalizeErr error
		turn, normalizeErr = state.normalize(payload)
		if normalizeErr != nil {
			status, code, message, param := responsesWebsocketErrorDetails(normalizeErr)
			writeError(status, code, message, param)
			continue
		}
		if len(turn.prewarmPayloads) > 0 {
			for _, responsePayload := range turn.prewarmPayloads {
				if writePayload(responsePayload) != nil {
					return
				}
			}
			var completed struct {
				Response struct {
					ID string `json:"id"`
				} `json:"response"`
			}
			_ = json.Unmarshal(turn.prewarmPayloads[len(turn.prewarmPayloads)-1], &completed)
			state.complete(turn, completed.Response.ID, []byte("[]"))
			continue
		}

		var ctx context.Context
		ctx, cancelTurn = context.WithCancel(sessionCtx)
		writer = newResponsesWebsocketHTTPWriter(conn, cancelTurn)
		writer.writePayload = writePayload
		finished = make(chan struct{})
		go func(ctx context.Context, w *responsesWebsocketHTTPWriter, body []byte, done chan struct{}) {
			defer close(done)
			defer func() {
				if recover() != nil {
					log.Print("responses websocket relay panicked")
					cancelSession()
				}
			}()
			execute(ctx, w, body)
			if err := w.Finish(); err != nil {
				cancelSession()
			}
		}(ctx, writer, turn.body, finished)
	}
}

func responsesWebsocketErrorDetails(err error) (int, string, string, string) {
	protocolErr, ok := err.(*responsesWebsocketProtocolError)
	if ok {
		return protocolErr.status, protocolErr.code, protocolErr.message, protocolErr.param
	}
	return http.StatusBadRequest, "invalid_request_error", err.Error(), ""
}

func executeResponsesWebsocketTurn(parent *gin.Context, writer *responsesWebsocketHTTPWriter, turnCtx context.Context, upstreamSession *responsesws.Session, body []byte) {
	if turnCtx.Err() != nil {
		return
	}
	c := newResponsesWebsocketTurnContext(parent, writer, turnCtx, upstreamSession, body)
	defer platformhttpx.CleanupBodyStorage(c)
	defer saveResponsesWebsocketRoutingContext(parent, c, upstreamSession)

	middleware.ModelRequestRateLimitWithHandler(
		middleware.DistributeWithHandler(func(c *gin.Context) {
			relayRequest(c, types.RelayFormatOpenAIResponses)
		}),
	)(c)
}

func newResponsesWebsocketTurnContext(parent *gin.Context, writer http.ResponseWriter, requestContext context.Context, upstreamSession *responsesws.Session, body []byte) *gin.Context {
	request, _ := http.NewRequestWithContext(requestContext, http.MethodPost, "/v1/responses", bytes.NewReader(body))
	request.Header = cloneResponsesWebsocketTurnHeaders(parent.Request.Header)
	request.Header.Set("Content-Type", "application/json")
	request.ContentLength = int64(len(body))
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}

	c, _ := gin.CreateTestContext(writer)
	c.Request = request
	c.Keys = cloneGinContextKeys(parent.Keys)
	if saved, exists := parent.Get(responsesWebsocketRoutingContextKey); exists && upstreamSession != nil {
		if snapshot, ok := saved.(responsesWebsocketRoutingContext); ok {
			channelID, keyIndex, bound := upstreamSession.Route()
			if bound && channelID == snapshot.ChannelID && keyIndex == snapshot.KeyIndex {
				applyResponsesRoutingContext(c, snapshot.Routing)
			}
		}
	}
	responsesws.Attach(c, upstreamSession)
	responsesws.ApplyRoutePin(c, upstreamSession)
	requestID := platformruntime.GetTimeString() + platformruntime.GetRandomString(8)
	traceID := parent.GetString(constant.TraceIdKey)
	c.Set(constant.RequestIdKey, requestID)
	c.Set(constant.TraceIdKey, traceID)
	httpctx.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
	requestContext = context.WithValue(c.Request.Context(), constant.RequestIdKey, requestID)
	requestContext = context.WithValue(requestContext, constant.TraceIdKey, traceID)
	c.Request = c.Request.WithContext(requestContext)
	return c
}

// Keep only the final pinned route, never a previous turn's reservation, retry
// budget, candidate list or request audit. An unbound session selects afresh.
func saveResponsesWebsocketRoutingContext(parent, turn *gin.Context, session *responsesws.Session) {
	if session == nil {
		return
	}
	channelID, keyIndex, bound := session.Route()
	if !bound {
		parent.Set(responsesWebsocketRoutingContextKey, nil)
		return
	}
	// A rejected turn can fail before channel setup. Keep the last valid route
	// while the upstream session is still pinned to it.
	if channelID != httpctx.GetContextKeyInt(turn, constant.ContextKeyChannelId) {
		return
	}
	parent.Set(responsesWebsocketRoutingContextKey, responsesWebsocketRoutingContext{
		ChannelID: channelID, KeyIndex: keyIndex, Routing: snapshotResponsesRoutingContext(turn),
	})
}

func cloneResponsesWebsocketTurnHeaders(source http.Header) http.Header {
	headers := source.Clone()
	for _, key := range []string{
		"Connection", "Upgrade", "Sec-WebSocket-Key", "Sec-WebSocket-Version",
		"Sec-WebSocket-Extensions", "Sec-WebSocket-Protocol", "Content-Length",
	} {
		headers.Del(key)
	}
	return headers
}

func cloneGinContextKeys(source map[any]any) map[any]any {
	cloned := make(map[any]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func responsesWebsocketErrorPayload(status int, code, message, param string) []byte {
	errorBody := map[string]any{"type": "invalid_request_error", "code": code, "message": message}
	if strings.TrimSpace(param) != "" {
		errorBody["param"] = param
	}
	payload, _ := json.Marshal(map[string]any{"type": "error", "status": status, "error": errorBody})
	return payload
}
