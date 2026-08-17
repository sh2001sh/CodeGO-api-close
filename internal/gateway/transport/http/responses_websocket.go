package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
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
	state := &responsesWebsocketState{}
	upstreamSession := responsesws.NewSession()
	defer upstreamSession.Close()

	for {
		if sessionCtx.Err() != nil {
			_ = writeResponsesWebsocketError(conn, http.StatusBadRequest, "websocket_connection_limit_reached", "Responses websocket connection limit reached (60 minutes). Create a new websocket connection to continue.", "")
			return
		}
		messageType, payload, readErr := conn.ReadMessage()
		if readErr != nil {
			if sessionCtx.Err() != nil {
				_ = writeResponsesWebsocketError(conn, http.StatusBadRequest, "websocket_connection_limit_reached", "Responses websocket connection limit reached (60 minutes). Create a new websocket connection to continue.", "")
			}
			return
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}

		turn, normalizeErr := state.normalize(payload)
		if normalizeErr != nil {
			status, code, message, param := responsesWebsocketErrorDetails(normalizeErr)
			_ = writeResponsesWebsocketError(conn, status, code, message, param)
			continue
		}
		if len(turn.prewarmPayloads) > 0 {
			for _, responsePayload := range turn.prewarmPayloads {
				if conn.WriteMessage(websocket.TextMessage, responsePayload) != nil {
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

		responseID, output, completed, executeErr := executeResponsesWebsocketTurn(c, conn, sessionCtx, upstreamSession, turn.body)
		if executeErr != nil {
			return
		}
		if completed {
			state.complete(turn, responseID, output)
		} else {
			state.fail(turn)
		}
	}
}

func responsesWebsocketErrorDetails(err error) (int, string, string, string) {
	protocolErr, ok := err.(*responsesWebsocketProtocolError)
	if ok {
		return protocolErr.status, protocolErr.code, protocolErr.message, protocolErr.param
	}
	return http.StatusBadRequest, "invalid_request_error", err.Error(), ""
}

func executeResponsesWebsocketTurn(parent *gin.Context, conn *websocket.Conn, sessionCtx context.Context, upstreamSession *responsesws.Session, body []byte) (string, []byte, bool, error) {
	turnCtx, cancelTurn := context.WithCancel(sessionCtx)
	defer cancelTurn()
	writer := newResponsesWebsocketHTTPWriter(conn, cancelTurn)
	c := newResponsesWebsocketTurnContext(parent, writer, turnCtx, upstreamSession, body)
	defer platformhttpx.CleanupBodyStorage(c)

	middleware.ModelRequestRateLimitWithHandler(
		middleware.DistributeWithHandler(func(c *gin.Context) {
			relayRequest(c, types.RelayFormatOpenAIResponses)
		}),
	)(c)
	if err := writer.Finish(); err != nil {
		return "", nil, false, err
	}
	responseID, output, completed := writer.Completion()
	return responseID, output, completed, nil
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

func cloneGinContextKeys(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func writeResponsesWebsocketError(conn *websocket.Conn, status int, code, message, param string) error {
	errorBody := map[string]any{"type": "invalid_request_error", "code": code, "message": message}
	if strings.TrimSpace(param) != "" {
		errorBody["param"] = param
	}
	payload, _ := json.Marshal(map[string]any{"type": "error", "status": status, "error": errorBody})
	return conn.WriteMessage(websocket.TextMessage, payload)
}
