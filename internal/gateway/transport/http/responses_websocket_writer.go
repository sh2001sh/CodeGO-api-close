package http

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

type responsesWebsocketHTTPWriter struct {
	mu             sync.Mutex
	conn           *websocket.Conn
	header         http.Header
	status         int
	buffer         bytes.Buffer
	writeErr       error
	cancel         func()
	closeNotify    chan bool
	responseID     string
	responseOutput []byte
	outputByIndex  map[int]json.RawMessage
	outputFallback []json.RawMessage
	completed      bool
	failed         bool
}

func newResponsesWebsocketHTTPWriter(conn *websocket.Conn, cancel func()) *responsesWebsocketHTTPWriter {
	return &responsesWebsocketHTTPWriter{
		conn: conn, header: make(http.Header), status: http.StatusOK,
		cancel: cancel, closeNotify: make(chan bool, 1), outputByIndex: make(map[int]json.RawMessage),
	}
}

func (w *responsesWebsocketHTTPWriter) Header() http.Header { return w.header }

func (w *responsesWebsocketHTTPWriter) WriteHeader(statusCode int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status = statusCode
}

func (w *responsesWebsocketHTTPWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	_, _ = w.buffer.Write(data)
	if strings.Contains(strings.ToLower(w.header.Get("Content-Type")), "text/event-stream") {
		w.drainSSELocked(false)
	}
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return len(data), nil
}

func (w *responsesWebsocketHTTPWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.drainSSELocked(false)
}

func (w *responsesWebsocketHTTPWriter) Finish() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.Contains(strings.ToLower(w.header.Get("Content-Type")), "text/event-stream") {
		w.drainSSELocked(true)
	} else if w.buffer.Len() > 0 {
		payload := buildResponsesWebsocketHTTPError(w.status, w.buffer.Bytes())
		w.sendPayloadLocked(payload)
		w.buffer.Reset()
	}
	return w.writeErr
}

func (w *responsesWebsocketHTTPWriter) Completion() (string, []byte, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.responseID, bytes.Clone(w.responseOutput), w.completed && !w.failed
}

func (w *responsesWebsocketHTTPWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("websocket response writer cannot be hijacked")
}

func (w *responsesWebsocketHTTPWriter) CloseNotify() <-chan bool { return w.closeNotify }

func (w *responsesWebsocketHTTPWriter) Push(string, *http.PushOptions) error {
	return http.ErrNotSupported
}

func (w *responsesWebsocketHTTPWriter) drainSSELocked(final bool) {
	for {
		data := w.buffer.Bytes()
		index, delimiterSize := sseEventBoundary(data)
		if index < 0 {
			if final && len(bytes.TrimSpace(data)) > 0 {
				index, delimiterSize = len(data), 0
			} else {
				return
			}
		}
		event := bytes.Clone(data[:index])
		w.buffer.Next(index + delimiterSize)
		payload := parseResponsesWebsocketSSEEvent(event)
		if len(payload) > 0 {
			w.sendPayloadLocked(payload)
		}
		if w.writeErr != nil {
			return
		}
	}
}

func (w *responsesWebsocketHTTPWriter) sendPayloadLocked(payload []byte) {
	if len(payload) == 0 || w.writeErr != nil {
		return
	}
	w.observePayloadLocked(payload)
	if err := w.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		w.writeErr = err
		if w.cancel != nil {
			w.cancel()
		}
		select {
		case w.closeNotify <- true:
		default:
		}
	}
}

func (w *responsesWebsocketHTTPWriter) observePayloadLocked(payload []byte) {
	var event struct {
		Type        string          `json:"type"`
		OutputIndex *int            `json:"output_index"`
		Item        json.RawMessage `json:"item"`
		Response    struct {
			ID     string          `json:"id"`
			Output json.RawMessage `json:"output"`
		} `json:"response"`
	}
	if json.Unmarshal(payload, &event) != nil {
		return
	}
	switch event.Type {
	case "response.output_item.done":
		if len(bytes.TrimSpace(event.Item)) == 0 || bytes.Equal(bytes.TrimSpace(event.Item), []byte("null")) {
			return
		}
		if event.OutputIndex != nil {
			w.outputByIndex[*event.OutputIndex] = bytes.Clone(event.Item)
		} else {
			w.outputFallback = append(w.outputFallback, bytes.Clone(event.Item))
		}
	case "response.completed":
		w.completed = true
		w.responseID = event.Response.ID
		w.responseOutput = bytes.Clone(event.Response.Output)
		if len(w.responseOutput) == 0 || bytes.Equal(bytes.TrimSpace(w.responseOutput), []byte("[]")) {
			w.responseOutput = w.collectedOutputLocked()
		}
	case "error", "response.failed", "response.incomplete":
		w.failed = true
	}
}

func (w *responsesWebsocketHTTPWriter) collectedOutputLocked() []byte {
	indexes := make([]int, 0, len(w.outputByIndex))
	for index := range w.outputByIndex {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	items := make([]json.RawMessage, 0, len(indexes)+len(w.outputFallback))
	for _, index := range indexes {
		items = append(items, w.outputByIndex[index])
	}
	items = append(items, w.outputFallback...)
	output, err := json.Marshal(items)
	if err != nil {
		return []byte("[]")
	}
	return output
}

func sseEventBoundary(data []byte) (int, int) {
	if index := bytes.Index(data, []byte("\n\n")); index >= 0 {
		return index, 2
	}
	if index := bytes.Index(data, []byte("\r\n\r\n")); index >= 0 {
		return index, 4
	}
	return -1, 0
}

func parseResponsesWebsocketSSEEvent(event []byte) []byte {
	lines := bytes.Split(event, []byte("\n"))
	dataLines := make([][]byte, 0, 1)
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		value := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if bytes.Equal(value, []byte("[DONE]")) {
			return nil
		}
		dataLines = append(dataLines, value)
	}
	payload := bytes.Join(dataLines, []byte("\n"))
	if !json.Valid(payload) {
		return nil
	}
	return payload
}

func buildResponsesWebsocketHTTPError(status int, body []byte) []byte {
	if status < 400 {
		status = http.StatusInternalServerError
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil {
		envelope = map[string]json.RawMessage{}
	}
	errorValue := envelope["error"]
	if len(errorValue) == 0 {
		errorValue, _ = json.Marshal(map[string]string{"type": "server_error", "message": http.StatusText(status)})
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "error", "status": status, "error": json.RawMessage(errorValue),
	})
	return payload
}
