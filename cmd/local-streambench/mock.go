package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sh2001sh/new-api/constant"
	"github.com/tiktoken-go/tokenizer/codec"
)

const (
	mockWSControlTimeout    = 2 * time.Second
	mockWSCloseDrainTimeout = 2 * time.Second
)

type mockRecord struct {
	ResponseID, GatewayRequestID, Path, Model, Protocol, Scenario              string
	BodyBytes, PromptBytes, InputTokens, TextFrames                            int
	BodyReadMS, PreparedMS, FirstFrameMS, FirstTextFlushMS, TerminalMS, DoneMS float64
	Transport                                                                  string
	Truncated, Failed, Cancelled                                               bool
	WSPingsReceived, WSPongsSent, WSPongsReceived                              int
	WSReadError, WSWriteError, WSControlError                                  string
	WSPeerCloseCode                                                            int
	WSCloseSent, WSCloseHandshake, WSCloseTimedOut                             bool
	CancelledMS                                                                float64
}
type mockServer struct {
	c       benchConfig
	mu      sync.Mutex
	records map[string]*mockRecord
	tokenMu sync.Mutex
	tokens  map[[32]byte]int
	calls   atomic.Int64
	active  atomic.Int64
	peak    atomic.Int64
}

func serveMock(c benchConfig) error {
	if err := c.validate(); err != nil {
		return err
	}
	m := newMock(c)
	fmt.Println("synthetic stream upstream: 127.0.0.1:18181; stats: /stats; no external forwarding")
	return (&http.Server{Addr: "127.0.0.1:18181", Handler: m, ReadHeaderTimeout: 10 * time.Second, MaxHeaderBytes: 64 << 10}).ListenAndServe()
}
func newMock(c benchConfig) *mockServer {
	return &mockServer{c: c, records: map[string]*mockRecord{}, tokens: map[[32]byte]int{}}
}
func (m *mockServer) update(id string, fn func(*mockRecord)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(m.records[id])
}
func elapsed(t time.Time) float64 { return float64(time.Since(t)) / float64(time.Millisecond) }
func waitContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (m *mockServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/stats" {
		m.mu.Lock()
		defer m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if id := r.URL.Query().Get("id"); id != "" {
			_ = json.NewEncoder(w).Encode(m.records[id])
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"calls": m.calls.Load(), "active": m.active.Load(), "peak": m.peak.Load(), "records": m.records, "timing": "all record stage durations use one upstream monotonic clock; do not subtract remote wall clocks"})
		return
	}
	protocol := ""
	switch {
	case r.URL.Path == "/v1/chat/completions":
		protocol = "chat"
	case r.URL.Path == "/v1/responses":
		protocol = "responses"
	case r.URL.Path == "/v1/messages":
		protocol = "claude"
	case strings.Contains(r.URL.Path, ":streamGenerateContent"):
		protocol = "gemini"
	}
	if protocol == "" {
		http.NotFound(w, r)
		return
	}
	start := time.Now()
	streamCtx, cancelStream := context.WithCancel(r.Context())
	defer cancelStream()
	n := m.calls.Add(1)
	id := fmt.Sprintf("resp_streambench_%08d", n)
	current := m.active.Add(1)
	defer m.active.Add(-1)
	for old := m.peak.Load(); current > old; old = m.peak.Load() {
		if m.peak.CompareAndSwap(old, current) {
			break
		}
	}
	record := &mockRecord{ResponseID: id, GatewayRequestID: r.Header.Get(constant.RequestIdKey), Path: r.URL.Path, Protocol: protocol, Transport: "sse"}
	m.mu.Lock()
	m.records[id] = record
	m.mu.Unlock()
	interruptStream := func() {
		cancelStream()
		m.update(id, func(s *mockRecord) {
			if s.CancelledMS == 0 && s.TerminalMS == 0 && !s.Truncated && !s.Failed {
				s.CancelledMS = elapsed(start)
			}
		})
	}
	defer func() {
		m.update(id, func(s *mockRecord) {
			s.DoneMS = elapsed(start)
			s.Cancelled = streamCtx.Err() != nil && s.TerminalMS == 0 && !s.Truncated && !s.Failed
		})
	}()
	var conn *websocket.Conn
	var body []byte
	var err error
	if websocket.IsWebSocketUpgrade(r) {
		if protocol != "responses" {
			http.Error(w, "only responses websocket", 400)
			return
		}
		conn, err = (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }, EnableCompression: false}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadLimit(8 << 20)
		_, body, err = conn.ReadMessage()
		m.update(id, func(s *mockRecord) { s.Transport = "websocket" })
		if err == nil {
			readerDone := make(chan struct{})
			var closeSent atomic.Bool
			// WriteControl is safe beside the handler's sole data writer.
			control := func(kind int, payload []byte) error {
				if kind == websocket.CloseMessage && !closeSent.CompareAndSwap(false, true) {
					return nil
				}
				e := conn.WriteControl(kind, payload, time.Now().Add(mockWSControlTimeout))
				m.update(id, func(s *mockRecord) {
					if e != nil {
						s.WSControlError = e.Error()
					} else if kind == websocket.CloseMessage {
						s.WSCloseSent = true
						s.WSCloseHandshake = s.WSPeerCloseCode != 0
					} else if kind == websocket.PongMessage {
						s.WSPongsSent++
					}
				})
				return e
			}
			conn.SetPingHandler(func(payload string) error {
				m.update(id, func(s *mockRecord) { s.WSPingsReceived++ })
				return control(websocket.PongMessage, []byte(payload))
			})
			conn.SetPongHandler(func(string) error {
				m.update(id, func(s *mockRecord) { s.WSPongsReceived++ })
				return nil
			})
			conn.SetCloseHandler(func(code int, text string) error {
				// Stop generating immediately; acknowledging Close can wait for
				// the data writer and must not delay cancellation of TTFT/tail.
				interruptStream()
				m.update(id, func(s *mockRecord) {
					s.WSPeerCloseCode = code
					s.WSCloseHandshake = s.WSCloseSent
				})
				return control(websocket.CloseMessage, websocket.FormatCloseMessage(code, ""))
			})
			go func() {
				defer close(readerDone)
				defer interruptStream()
				for {
					_, _, readErr := conn.ReadMessage()
					if readErr != nil {
						if !websocket.IsCloseError(readErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
							m.update(id, func(s *mockRecord) { s.WSReadError = readErr.Error() })
						}
						return
					}
					m.update(id, func(s *mockRecord) { s.WSReadError = "unexpected second request on single-stream mock connection" })
					return
				}
			}()
			defer func() {
				_ = control(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				timer := time.NewTimer(mockWSCloseDrainTimeout)
				defer timer.Stop()
				select {
				case <-readerDone:
				case <-timer.C:
					m.update(id, func(s *mockRecord) { s.WSCloseTimedOut = true })
				}
				_ = conn.Close()
				<-readerDone
			}()
		}
	} else {
		body, err = io.ReadAll(io.LimitReader(r.Body, 8<<20+1))
		defer r.Body.Close()
	}
	if err != nil || len(body) > 8<<20 {
		if conn == nil {
			http.Error(w, "invalid or oversized request", 400)
		}
		return
	}
	m.update(id, func(s *mockRecord) { s.BodyReadMS = elapsed(start); s.BodyBytes = len(body) })
	var request map[string]any
	if json.Unmarshal(body, &request) != nil {
		if conn == nil {
			http.Error(w, "invalid JSON", 400)
		}
		return
	}
	prompt := extractPrompt(request)
	scenario := "text"
	for _, value := range []string{"thinking", "tool"} {
		if strings.Contains(prompt, "STREAMBENCH scenario="+value) {
			scenario = value
		}
	}
	hash := sha256.Sum256([]byte(prompt))
	m.tokenMu.Lock()
	input, ok := m.tokens[hash]
	if !ok {
		input, err = codec.NewCl100kBase().Count(prompt)
		if err == nil {
			m.tokens[hash] = input
		}
	}
	m.tokenMu.Unlock()
	if err != nil {
		return
	}
	c := m.c
	c.Scenario = scenario
	wanted := expectedWorkload(c)
	wanted.InputTokens = input
	m.update(id, func(s *mockRecord) {
		s.PreparedMS = elapsed(start)
		s.Model = str(request["model"])
		s.Scenario = scenario
		s.InputTokens = input
		s.PromptBytes = len(prompt)
	})
	if conn == nil {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
	}
	write := func(event map[string]any) bool {
		data, e := json.Marshal(event)
		if e != nil {
			return false
		}
		if conn != nil {
			e = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if e == nil {
				e = conn.WriteMessage(websocket.TextMessage, data)
			}
			if e != nil {
				m.update(id, func(s *mockRecord) { s.WSWriteError = e.Error() })
				interruptStream()
			}
		} else {
			if protocol == "claude" || protocol == "responses" {
				_, e = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", str(event["type"]), data)
			} else {
				_, e = fmt.Fprintf(w, "data: %s\n\n", data)
			}
			if e == nil {
				e = http.NewResponseController(w).Flush()
			}
		}
		if e == nil {
			m.update(id, func(s *mockRecord) {
				if s.FirstFrameMS == 0 {
					s.FirstFrameMS = elapsed(start)
				}
			})
		}
		return e == nil
	}
	for _, event := range preamble(protocol, id, c.Model, wanted) {
		if !write(event) {
			return
		}
	}
	if !waitContext(streamCtx, c.TTFT) {
		return
	}
	for i := 0; i < wanted.Frames; i++ {
		if i > 0 && !waitContext(streamCtx, c.Interval) {
			return
		}
		for _, event := range contentEvents(protocol, id, c.Model, i, c.Scenario, wanted.Frames, c.ChunkBytes) {
			if !write(event) {
				return
			}
		}
		m.update(id, func(s *mockRecord) {
			s.TextFrames++
			if s.FirstTextFlushMS == 0 {
				s.FirstTextFlushMS = elapsed(start)
			}
		})
		if c.TruncateEvery > 0 && n%int64(c.TruncateEvery) == 0 && i >= wanted.Frames/2 {
			m.update(id, func(s *mockRecord) { s.Truncated = true })
			return
		}
	}
	if c.ErrorEvery > 0 && n%int64(c.ErrorEvery) == 0 {
		write(map[string]any{"type": "error", "error": map[string]any{"type": "server_error", "message": "synthetic failed terminal"}})
		m.update(id, func(s *mockRecord) { s.Failed = true })
		return
	}
	for _, event := range terminalEvents(protocol, id, c.Model, c.Scenario, wanted) {
		if !write(event) {
			return
		}
	}
	if protocol == "chat" && conn == nil {
		_, err = io.WriteString(w, "data: [DONE]\n\n")
		if err == nil {
			_ = http.NewResponseController(w).Flush()
		}
	}
	m.update(id, func(s *mockRecord) { s.TerminalMS = elapsed(start) })
}

func expectedWorkload(c benchConfig) workload {
	w := workload{Frames: frameCount(c)}
	for i := 0; i < w.Frames; i++ {
		w.Text += sizedTextChunk(i, c.ChunkBytes)
		if c.Scenario == "thinking" {
			w.Thought += thoughtChunk(i)
		}
	}
	if c.Scenario == "tool" {
		w.Tool = toolArguments
	}
	w.OutputTokens, _ = codec.NewCl100kBase().Count(w.Text)
	return w
}
func extractPrompt(m map[string]any) string {
	if s := str(m["input"]); s != "" {
		return s
	}
	for _, key := range []string{"messages", "input", "contents"} {
		for _, raw := range list(m[key]) {
			item := object(raw)
			if s := str(item["content"]); s != "" {
				return s
			}
			for _, partKey := range []string{"content", "parts"} {
				for _, part := range list(item[partKey]) {
					if text := str(object(part)["text"]); text != "" {
						return text
					}
				}
			}
		}
	}
	return ""
}
func preamble(protocol, id, model string, w workload) []map[string]any {
	switch protocol {
	case "chat":
		return []map[string]any{{"id": id, "object": "chat.completion.chunk", "model": model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil}}}}
	case "responses":
		return []map[string]any{{"type": "response.created", "response": map[string]any{"id": id, "object": "response", "status": "in_progress", "model": model, "output": []any{}}}, {"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": "msg_" + id, "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}}, {"type": "response.content_part.added", "item_id": "msg_" + id, "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}}}
	case "claude":
		return []map[string]any{{"type": "message_start", "message": map[string]any{"id": id, "type": "message", "role": "assistant", "model": model, "content": []any{}, "stop_reason": nil, "usage": map[string]any{"input_tokens": w.InputTokens, "output_tokens": 0}}}, {"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}}}
	case "gemini":
		return []map[string]any{{"responseId": id, "candidates": []any{map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": []any{}}}}}}
	}
	return nil
}
func contentEvents(protocol, id, model string, i int, scenario string, total, chunkBytes int) []map[string]any {
	text := sizedTextChunk(i, chunkBytes)
	tool := ""
	if scenario == "tool" {
		runes := []rune(toolArguments)
		start, end := len(runes)*i/total, len(runes)*(i+1)/total
		tool = string(runes[start:end])
	}
	switch protocol {
	case "chat":
		d := map[string]any{"content": text}
		if scenario == "thinking" {
			d["reasoning_content"] = thoughtChunk(i)
		}
		if scenario == "tool" {
			fn := map[string]any{"arguments": tool}
			t := map[string]any{"index": 0, "function": fn}
			if i == 0 {
				fn["name"] = "capture"
				t["id"] = "call_fixture"
				t["type"] = "function"
			}
			d["tool_calls"] = []any{t}
		}
		return []map[string]any{{"id": id, "object": "chat.completion.chunk", "model": model, "choices": []any{map[string]any{"index": 0, "delta": d, "finish_reason": nil}}}}
	case "responses":
		events := []map[string]any{{"type": "response.output_text.delta", "item_id": "msg_" + id, "output_index": 0, "content_index": 0, "delta": text}}
		if scenario == "thinking" {
			if i == 0 {
				events = append(events, map[string]any{"type": "response.output_item.added", "output_index": 1, "item": map[string]any{"id": "reason_" + id, "type": "reasoning", "summary": []any{}}}, map[string]any{"type": "response.reasoning_summary_part.added", "item_id": "reason_" + id, "output_index": 1, "summary_index": 0, "part": map[string]any{"type": "summary_text", "text": ""}})
			}
			events = append(events, map[string]any{"type": "response.reasoning_summary_text.delta", "item_id": "reason_" + id, "output_index": 1, "summary_index": 0, "delta": thoughtChunk(i)})
		}
		if scenario == "tool" {
			if i == 0 {
				events = append(events, map[string]any{"type": "response.output_item.added", "output_index": 1, "item": map[string]any{"type": "function_call", "id": "fc_" + id, "call_id": "call_fixture", "name": "capture", "arguments": "", "status": "in_progress"}})
			}
			events = append(events, map[string]any{"type": "response.function_call_arguments.delta", "item_id": "fc_" + id, "output_index": 1, "delta": tool})
		}
		return events
	case "claude":
		events := []map[string]any{{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}}}
		if scenario != "text" {
			kind := scenario
			if scenario == "tool" {
				kind = "tool_use"
			}
			if i == 0 {
				block := map[string]any{"type": kind, "thinking": ""}
				if scenario == "tool" {
					block = map[string]any{"type": "tool_use", "id": "call_fixture", "name": "capture", "input": map[string]any{}}
				}
				events = append(events, map[string]any{"type": "content_block_start", "index": 1, "content_block": block})
			}
			delta := map[string]any{"type": "thinking_delta", "thinking": thoughtChunk(i)}
			if scenario == "tool" {
				delta = map[string]any{"type": "input_json_delta", "partial_json": tool}
			}
			events = append(events, map[string]any{"type": "content_block_delta", "index": 1, "delta": delta})
		}
		return events
	case "gemini":
		parts := []any{map[string]any{"text": text}}
		if scenario == "thinking" {
			parts = append(parts, map[string]any{"text": thoughtChunk(i), "thought": true})
		}
		if scenario == "tool" && i == 0 {
			var args any
			_ = json.Unmarshal([]byte(toolArguments), &args)
			parts = append(parts, map[string]any{"functionCall": map[string]any{"name": "capture", "args": args}})
		}
		return []map[string]any{{"responseId": id, "candidates": []any{map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": parts}}}}}
	}
	return nil
}
func terminalEvents(protocol, id, model, scenario string, w workload) []map[string]any {
	usage := map[string]any{"input_tokens": w.InputTokens, "output_tokens": w.OutputTokens, "total_tokens": w.InputTokens + w.OutputTokens}
	switch protocol {
	case "chat":
		reason := "stop"
		if scenario == "tool" {
			reason = "tool_calls"
		}
		return []map[string]any{{"id": id, "object": "chat.completion.chunk", "model": model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": reason}}, "usage": map[string]any{"prompt_tokens": w.InputTokens, "completion_tokens": w.OutputTokens, "total_tokens": w.InputTokens + w.OutputTokens}}}
	case "responses":
		item := map[string]any{"id": "msg_" + id, "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": w.Text, "annotations": []any{}}}}
		events := []map[string]any{{"type": "response.output_text.done", "item_id": "msg_" + id, "output_index": 0, "content_index": 0, "text": w.Text}, {"type": "response.content_part.done", "item_id": "msg_" + id, "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": w.Text, "annotations": []any{}}}, {"type": "response.output_item.done", "output_index": 0, "item": item}}
		output := []any{item}
		if scenario == "tool" {
			tool := map[string]any{"type": "function_call", "id": "fc_" + id, "call_id": "call_fixture", "name": "capture", "arguments": w.Tool, "status": "completed"}
			output = append(output, tool)
			events = append(events, map[string]any{"type": "response.function_call_arguments.done", "item_id": "fc_" + id, "output_index": 1, "arguments": w.Tool}, map[string]any{"type": "response.output_item.done", "output_index": 1, "item": tool})
		}
		if scenario == "thinking" {
			part := map[string]any{"type": "summary_text", "text": w.Thought}
			reason := map[string]any{"id": "reason_" + id, "type": "reasoning", "summary": []any{part}}
			output = append(output, reason)
			events = append(events, map[string]any{"type": "response.reasoning_summary_text.done", "item_id": "reason_" + id, "output_index": 1, "summary_index": 0, "text": w.Thought}, map[string]any{"type": "response.reasoning_summary_part.done", "item_id": "reason_" + id, "output_index": 1, "summary_index": 0, "part": part}, map[string]any{"type": "response.output_item.done", "output_index": 1, "item": reason})
		}
		return append(events, map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "object": "response", "status": "completed", "model": model, "output": output, "usage": usage}})
	case "claude":
		events := []map[string]any{{"type": "content_block_stop", "index": 0}}
		if scenario != "text" {
			events = append(events, map[string]any{"type": "content_block_stop", "index": 1})
		}
		reason := "end_turn"
		if scenario == "tool" {
			reason = "tool_use"
		}
		return append(events, map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": reason, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": w.OutputTokens}}, map[string]any{"type": "message_stop"})
	case "gemini":
		return []map[string]any{{"responseId": id, "modelVersion": model, "candidates": []any{map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": []any{}}, "finishReason": "STOP"}}, "usageMetadata": map[string]any{"promptTokenCount": w.InputTokens, "candidatesTokenCount": w.OutputTokens, "totalTokenCount": w.InputTokens + w.OutputTokens}}}
	}
	return nil
}
