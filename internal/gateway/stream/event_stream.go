package stream

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
	"io"
	"net/http"
	"strings"
)

func markResponseBodyDelivered(c *gin.Context) {
	if c == nil {
		return
	}
	httpctx.SetContextKey(c, constant.ContextKeyResponseBodyDelivered, true)
}

func FlushWriter(c *gin.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("flush panic recovered: %v", r)
		}
	}()

	if c == nil || c.Writer == nil {
		return nil
	}
	if c.Request != nil && c.Request.Context().Err() != nil {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}
	if err := StreamWorkerContext(c).Err(); err != nil {
		return fmt.Errorf("stream worker context done: %w", err)
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming error: flusher not found")
	}
	if value, exists := c.Get(gatewayruntime.FirstByteTraceContextKey); exists {
		if trace, ok := value.(*gatewayruntime.FirstByteTrace); ok {
			trace.MarkFirstFlush()
		}
	}
	flusher.Flush()
	return nil
}

// FlushHeaders commits only the HTTP streaming headers. It deliberately does
// not mark response body delivery, so a pre-semantic upstream failure can
// remain retry-safe.
func FlushHeaders(c *gin.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("header flush panic recovered: %v", r)
		}
	}()
	if c == nil || c.Writer == nil {
		return nil
	}
	if c.Request != nil && c.Request.Context().Err() != nil {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}
	if err := StreamWorkerContext(c).Err(); err != nil {
		return fmt.Errorf("stream worker context done: %w", err)
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming error: flusher not found")
	}
	if value, exists := c.Get(gatewayruntime.FirstByteTraceContextKey); exists {
		if trace, ok := value.(*gatewayruntime.FirstByteTrace); ok {
			trace.MarkHeadersFlush()
		}
	}
	flusher.Flush()
	return nil
}

// writeSSEParts writes an SSE event in one downstream write. Keeping the
// existing CustomEvent framing rules here avoids two Render calls per token
// while preserving carriage-return escaping and data-frame termination.
func writeSSEParts(c *gin.Context, parts ...string) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}
	CustomEvent{}.WriteContentType(c.Writer)
	var payload strings.Builder
	for _, part := range parts {
		customEventDataReplacer.WriteString(&payload, part)
		if strings.HasPrefix(part, "data") {
			payload.WriteString("\n\n")
		}
	}
	if _, err := io.WriteString(c.Writer, payload.String()); err != nil {
		return fmt.Errorf("write SSE data failed: %w", err)
	}
	return FlushWriter(c)
}

func SetEventStreamHeaders(c *gin.Context) {
	if _, exists := c.Get("event_stream_headers_set"); exists {
		return
	}
	c.Set("event_stream_headers_set", true)
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
}

func IsClientGone(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.Context().Err() != nil {
		MarkClientGone(c)
		return true
	}
	return false
}

// MarkClientGone records a downstream disconnect for the relay error path.
// It is intentionally request-local and never exposed to API consumers.
func MarkClientGone(c *gin.Context) {
	if c != nil {
		c.Set(string(constant.ContextKeyClientGone), true)
	}
}

func ClaudeData(c *gin.Context, resp dto.ClaudeResponse) error {
	if IsClientGone(c) {
		return fmt.Errorf("request context done")
	}
	jsonData, err := platformencoding.Marshal(resp)
	if err == nil {
		err = writeSSEParts(c, fmt.Sprintf("event: %s\n", resp.Type), "data: "+string(jsonData))
	}
	if err == nil {
		markResponseBodyDelivered(c)
	}
	return err
}

func ClaudeChunkData(c *gin.Context, resp dto.ClaudeResponse, data string) error {
	if IsClientGone(c) {
		return fmt.Errorf("request context done")
	}
	err := writeSSEParts(c, fmt.Sprintf("event: %s\n", resp.Type), fmt.Sprintf("data: %s\n", data))
	if err == nil {
		markResponseBodyDelivered(c)
	}
	return err
}

func ResponseChunkData(c *gin.Context, resp dto.ResponsesStreamResponse, data string) error {
	if IsClientGone(c) {
		return fmt.Errorf("request context done")
	}
	err := writeSSEParts(c, fmt.Sprintf("event: %s\n", resp.Type), fmt.Sprintf("data: %s", data))
	if err != nil && IsClientGone(c) {
		return err
	}
	if err == nil {
		markResponseBodyDelivered(c)
	}
	return err
}

func StringData(c *gin.Context, str string) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}
	if c.Request != nil && c.Request.Context().Err() != nil {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}
	err := writeSSEParts(c, "data: "+str)
	if err == nil && str != "[DONE]" {
		markResponseBodyDelivered(c)
	}
	return err
}

func PingData(c *gin.Context) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}
	if c.Request != nil && c.Request.Context().Err() != nil {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}
	if _, err := c.Writer.Write([]byte(": PING\n\n")); err != nil {
		return fmt.Errorf("write ping data failed: %w", err)
	}
	return FlushWriter(c)
}

func ObjectData(c *gin.Context, object interface{}) error {
	if object == nil {
		return errors.New("object is nil")
	}
	jsonData, err := platformencoding.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	return StringData(c, string(jsonData))
}

func Done(c *gin.Context) {
	if IsClientGone(c) {
		return
	}
	_ = StringData(c, "[DONE]")
}

func WssString(c *gin.Context, ws *websocket.Conn, str string) error {
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	return ws.WriteMessage(websocket.TextMessage, []byte(str))
}

func WssObject(c *gin.Context, ws *websocket.Conn, object interface{}) error {
	jsonData, err := platformencoding.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	return ws.WriteMessage(websocket.TextMessage, jsonData)
}

func WssError(c *gin.Context, ws *websocket.Conn, openaiError types.OpenAIError) {
	if ws == nil {
		return
	}
	errorObj := &dto.RealtimeEvent{
		Type:    "error",
		EventId: GetLocalRealtimeID(c),
		Error:   &openaiError,
	}
	_ = WssObject(c, ws, errorObj)
}

func GetResponseID(c *gin.Context) string {
	logID := c.GetString(constant.RequestIdKey)
	return fmt.Sprintf("chatcmpl-%s", logID)
}

func GetLocalRealtimeID(c *gin.Context) string {
	logID := c.GetString(constant.RequestIdKey)
	return fmt.Sprintf("evt_%s", logID)
}

func GenerateStartEmptyResponse(id string, createAt int64, model string, systemFingerprint *string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: systemFingerprint,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Role:    "assistant",
					Content: platformruntime.GetPointer(""),
				},
			},
		},
	}
}

func GenerateStopResponse(id string, createAt int64, model string, finishReason string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:      id,
		Object:  "chat.completion.chunk",
		Created: createAt,
		Model:   model,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				FinishReason: &finishReason,
			},
		},
	}
}

func GenerateFinalUsageResponse(id string, createAt int64, model string, usage dto.Usage) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:      id,
		Object:  "chat.completion.chunk",
		Created: createAt,
		Model:   model,
		Choices: make([]dto.ChatCompletionsStreamResponseChoice, 0),
		Usage:   &usage,
	}
}
