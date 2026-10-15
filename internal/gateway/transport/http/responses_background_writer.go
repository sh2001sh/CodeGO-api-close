package http

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
)

type responsesBackgroundWriter struct {
	mu             sync.Mutex
	job            *gatewayschema.ResponsesBackgroundJob
	header         http.Header
	statusCode     int
	buffer         bytes.Buffer
	nextSequence   int64
	writeErr       error
	terminal       bool
	terminalStatus string
	finalResponse  []byte
	errorValue     []byte
	upstreamID     string
}

func newResponsesBackgroundWriter(job *gatewayschema.ResponsesBackgroundJob) *responsesBackgroundWriter {
	nextSequence := int64(0)
	if job != nil && job.LastSequence >= 0 {
		nextSequence = job.LastSequence + 1
	}
	return &responsesBackgroundWriter{
		job: job, header: make(http.Header), statusCode: http.StatusOK, nextSequence: nextSequence,
	}
}

func (w *responsesBackgroundWriter) Header() http.Header { return w.header }

func (w *responsesBackgroundWriter) WriteHeader(statusCode int) {
	w.mu.Lock()
	w.statusCode = statusCode
	w.mu.Unlock()
}

func (w *responsesBackgroundWriter) Write(data []byte) (int, error) {
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

func (w *responsesBackgroundWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.drainSSELocked(false)
}

func (w *responsesBackgroundWriter) Finish(canceled bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !canceled && w.job != nil {
		requested, err := gatewaystore.ResponsesBackgroundCancelRequested(w.job.ID)
		if err == nil {
			canceled = requested
		}
	}
	if strings.Contains(strings.ToLower(w.header.Get("Content-Type")), "text/event-stream") {
		w.drainSSELocked(true)
	} else if w.buffer.Len() > 0 && !w.terminal {
		w.persistHTTPErrorLocked(w.buffer.Bytes())
		w.buffer.Reset()
	}
	if canceled && w.terminalStatus != gatewayschema.ResponsesBackgroundCompleted {
		w.terminal = false
		w.persistSyntheticTerminalLocked(gatewayschema.ResponsesBackgroundCanceled, "response.cancelled", nil)
	}
	if !w.terminal {
		w.persistSyntheticTerminalLocked(
			gatewayschema.ResponsesBackgroundFailed,
			"response.failed",
			map[string]any{"type": "server_error", "message": "Background response ended without a terminal event."},
		)
	}
	if w.writeErr != nil {
		return w.writeErr
	}
	finalCiphertext, err := platformsecurity.EncryptSecret(string(w.finalResponse))
	if err != nil {
		return err
	}
	errorCiphertext, err := platformsecurity.EncryptSecret(string(w.errorValue))
	if err != nil {
		return err
	}
	return gatewaystore.UpdateResponsesBackgroundTerminal(
		w.job.ID, w.terminalStatus, finalCiphertext, errorCiphertext, w.upstreamID, time.Now().UTC(),
	)
}

func (w *responsesBackgroundWriter) CompletionStatus() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.terminalStatus
}

func (w *responsesBackgroundWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("background response writer cannot be hijacked")
}

func (w *responsesBackgroundWriter) CloseNotify() <-chan bool {
	return make(chan bool)
}

func (w *responsesBackgroundWriter) Push(string, *http.PushOptions) error {
	return http.ErrNotSupported
}

func (w *responsesBackgroundWriter) drainSSELocked(final bool) {
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
			w.persistPayloadLocked(payload)
		}
		if w.writeErr != nil {
			return
		}
	}
}

func (w *responsesBackgroundWriter) persistPayloadLocked(payload []byte) {
	var event map[string]any
	if err := platformencoding.Unmarshal(payload, &event); err != nil {
		return
	}
	eventType, _ := event["type"].(string)
	response, _ := event["response"].(map[string]any)
	if response != nil {
		if upstreamID, _ := response["id"].(string); upstreamID != "" && w.upstreamID == "" {
			w.upstreamID = upstreamID
		}
		response["id"] = w.job.ID
		response["background"] = true
	}
	event["sequence_number"] = w.nextSequence
	normalized, err := platformencoding.Marshal(event)
	if err != nil {
		w.writeErr = err
		return
	}
	if err := persistBackgroundEvent(w.job.ID, w.nextSequence, eventType, normalized); err != nil {
		w.writeErr = err
		return
	}
	w.nextSequence++
	switch eventType {
	case "response.completed":
		w.setTerminalLocked(gatewayschema.ResponsesBackgroundCompleted, response, nil)
	case "response.failed", "response.incomplete":
		w.setTerminalLocked(gatewayschema.ResponsesBackgroundFailed, response, event["error"])
	case "error":
		w.setTerminalLocked(gatewayschema.ResponsesBackgroundFailed, nil, event["error"])
	}
}

func (w *responsesBackgroundWriter) persistHTTPErrorLocked(body []byte) {
	var envelope map[string]any
	_ = platformencoding.Unmarshal(body, &envelope)
	errorValue := envelope["error"]
	if errorValue == nil {
		errorValue = map[string]any{"type": "server_error", "message": http.StatusText(w.statusCode)}
	}
	w.persistSyntheticTerminalLocked(gatewayschema.ResponsesBackgroundFailed, "response.failed", errorValue)
}

func (w *responsesBackgroundWriter) persistSyntheticTerminalLocked(status, eventType string, errorValue any) {
	response := syntheticBackgroundResponse(w.job, status, errorValue)
	event := map[string]any{
		"type": eventType, "sequence_number": w.nextSequence, "response": response,
	}
	if errorValue != nil {
		event["error"] = errorValue
	}
	payload, err := platformencoding.Marshal(event)
	if err != nil {
		w.writeErr = err
		return
	}
	if err := persistBackgroundEvent(w.job.ID, w.nextSequence, eventType, payload); err != nil {
		w.writeErr = err
		return
	}
	w.nextSequence++
	w.setTerminalLocked(status, response, errorValue)
}

func (w *responsesBackgroundWriter) setTerminalLocked(status string, response map[string]any, errorValue any) {
	if w.terminal {
		return
	}
	if response == nil {
		response = syntheticBackgroundResponse(w.job, status, errorValue)
	}
	response["id"] = w.job.ID
	response["background"] = true
	response["status"] = status
	w.finalResponse, _ = platformencoding.Marshal(response)
	if errorValue != nil {
		w.errorValue, _ = platformencoding.Marshal(errorValue)
	}
	w.terminal = true
	w.terminalStatus = status
}

func persistBackgroundEvent(jobID string, sequence int64, eventType string, payload []byte) error {
	ciphertext, err := platformsecurity.EncryptSecret(string(payload))
	if err != nil {
		return err
	}
	return gatewaystore.AppendResponsesBackgroundEvent(&gatewayschema.ResponsesBackgroundEvent{
		JobID: jobID, Sequence: sequence, Type: eventType, PayloadCiphertext: ciphertext,
	})
}

func syntheticBackgroundResponse(job *gatewayschema.ResponsesBackgroundJob, status string, errorValue any) map[string]any {
	createdAt := time.Now().Unix()
	model := ""
	jobID := ""
	if job != nil {
		jobID = job.ID
		model = job.Model
		if !job.CreatedAt.IsZero() {
			createdAt = job.CreatedAt.Unix()
		}
	}
	return map[string]any{
		"id": jobID, "object": "response", "created_at": createdAt,
		"model": model, "background": true, "status": status,
		"output": []any{}, "error": errorValue,
	}
}

func appendSyntheticBackgroundTerminal(job *gatewayschema.ResponsesBackgroundJob, status, eventType string, errorValue any) error {
	if job == nil {
		return errors.New("background job is nil")
	}
	sequence := job.LastSequence + 1
	event := map[string]any{
		"type": eventType, "sequence_number": sequence,
		"response": syntheticBackgroundResponse(job, status, errorValue),
	}
	if errorValue != nil {
		event["error"] = errorValue
	}
	payload, err := platformencoding.Marshal(event)
	if err != nil {
		return err
	}
	return persistBackgroundEvent(job.ID, sequence, eventType, payload)
}
