package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/tiktoken-go/tokenizer/codec"
)

const toolArguments = `{"value":"synthetic-tool-value-流式"}`

type workload struct {
	Body                                           []byte
	Path, Text, Thought, Tool                      string
	InputTokens, OutputTokens, PromptBytes, Frames int
}

func textChunk(index int) string { return fmt.Sprintf(" segment-%03d 合流🌊", index) }
func sizedTextChunk(index, extra int) string {
	return textChunk(index) + strings.Repeat(" light ", (extra+6)/7)[:extra]
}
func thoughtChunk(index int) string { return fmt.Sprintf(" reason-%03d", index) }

func frameCount(c benchConfig) int { return int(c.Tail/c.Interval) + 1 }

func makeWorkload(c benchConfig) (workload, error) {
	enc := codec.NewCl100kBase()
	// The marker survives ordinary protocol conversion and selects only mock behavior.
	prefix := "STREAMBENCH scenario=" + c.Scenario + "\n"
	pattern := "# Request processing\n```go\nfunc serve(ctx context.Context) error { return streamResponse(ctx) }\n```\nOrdered output requires complete text and an atomic settlement.\n"
	ids, pieces, err := enc.Encode(prefix + strings.Repeat(pattern, c.InputTokens/20+2))
	if err != nil {
		return workload{}, err
	}
	if len(ids) < c.InputTokens {
		return workload{}, errors.New("insufficient prompt tokens")
	}
	// Join encoded pieces without Decode's quadratic appends on large inputs.
	// The full content is still verified by the reference tokenizer below.
	prompt := strings.Join(pieces[:c.InputTokens], "")
	count, err := enc.Count(prompt)
	if err != nil {
		return workload{}, err
	}
	if count != c.InputTokens {
		return workload{}, fmt.Errorf("prompt roundtrip token mismatch: %d != %d", count, c.InputTokens)
	}
	w := workload{InputTokens: count, PromptBytes: len(prompt), Frames: frameCount(c)}
	for i := 0; i < w.Frames; i++ {
		w.Text += sizedTextChunk(i, c.ChunkBytes)
		if c.Scenario == "thinking" {
			w.Thought += thoughtChunk(i)
		}
	}
	if c.Scenario == "tool" {
		w.Tool = toolArguments
	}
	w.OutputTokens, err = enc.Count(w.Text)
	if err != nil {
		return w, err
	}
	outputBudget := max(8192, w.OutputTokens+4096)
	p := map[string]any{"model": c.Model, "stream": true}
	switch c.Protocol {
	case "chat":
		w.Path = "/v1/chat/completions"
		p["messages"] = []any{map[string]any{"role": "user", "content": prompt}}
		p["max_tokens"] = outputBudget
		p["stream_options"] = map[string]bool{"include_usage": true}
	case "responses", "ws":
		w.Path = "/v1/responses"
		p["input"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": prompt}}}}
		p["store"] = false
		p["max_output_tokens"] = outputBudget
		if c.Protocol == "ws" {
			delete(p, "stream")
			p["type"] = "response.create"
		}
	case "claude":
		w.Path = "/v1/messages"
		p["messages"] = []any{map[string]any{"role": "user", "content": prompt}}
		p["max_tokens"] = outputBudget
	case "gemini":
		w.Path = "/v1beta/models/" + url.PathEscape(c.Model) + ":streamGenerateContent?alt=sse"
		p = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": prompt}}}}, "generationConfig": map[string]any{"maxOutputTokens": outputBudget}}
	}
	w.Body, err = json.Marshal(p)
	return w, err
}

type observation struct {
	Index          int     `json:"index"`
	RequestID      string  `json:"request_id"`
	MockResponseID string  `json:"mock_response_id"`
	Status         int     `json:"status"`
	OK             bool    `json:"ok"`
	Error          string  `json:"error,omitempty"`
	FirstRawMS     float64 `json:"first_raw_frame_ms"`
	FirstTextMS    float64 `json:"first_text_ms"`
	CompleteMS     float64 `json:"complete_ms"`
	MaxGapMS       float64 `json:"max_text_gap_ms"`
	Frames         int     `json:"text_frames"`
	RawFrames      int     `json:"raw_frames"`
	TextBytes      int     `json:"text_bytes"`
	ThoughtBytes   int     `json:"thought_bytes"`
	ToolBytes      int     `json:"tool_bytes"`
	InputTokens    int     `json:"usage_input_tokens"`
	OutputTokens   int     `json:"usage_output_tokens"`
	Terminal       string  `json:"terminal"`
}

type streamState struct {
	o                   *observation
	start, last         time.Time
	text, thought, tool strings.Builder
	terminal, done      bool
	textParts           map[string]*strings.Builder
	completedTextParts  map[string]string
}

func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func str(v any) string            { s, _ := v.(string); return s }
func num(v any) int               { f, _ := v.(float64); return int(f) }
func list(v any) []any            { a, _ := v.([]any); return a }
func (s *streamState) content(text string) {
	if text == "" {
		return
	}
	now := time.Now()
	if s.o.Frames == 0 {
		s.o.FirstTextMS = float64(now.Sub(s.start)) / float64(time.Millisecond)
	} else if gap := float64(now.Sub(s.last)) / float64(time.Millisecond); gap > s.o.MaxGapMS {
		s.o.MaxGapMS = gap
	}
	s.last = now
	s.o.Frames++
	s.text.WriteString(text)
}
func (s *streamState) usage(m map[string]any, in, out string) {
	if v := num(m[in]); v > 0 {
		s.o.InputTokens = v
	}
	if v := num(m[out]); v > 0 {
		s.o.OutputTokens = v
	}
}
func (s *streamState) frame(data []byte, protocol string) error {
	s.o.RawFrames++
	if s.o.RawFrames == 1 {
		s.o.FirstRawMS = float64(time.Since(s.start)) / float64(time.Millisecond)
	}
	if strings.TrimSpace(string(data)) == "[DONE]" {
		s.done = true
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("invalid stream JSON: %w", err)
	}
	if protocol == "responses" || protocol == "ws" {
		if id := str(m["response_id"]); id != "" {
			s.o.MockResponseID = id
		}
		if id := str(object(m["response"])["id"]); id != "" {
			s.o.MockResponseID = id
		}
	}
	if e := m["error"]; e != nil {
		return fmt.Errorf("stream error: %v", e)
	}
	if id := str(m["id"]); id != "" {
		s.o.MockResponseID = id
	}
	switch protocol {
	case "chat":
		for _, raw := range list(m["choices"]) {
			ch := object(raw)
			d := object(ch["delta"])
			s.content(str(d["content"]))
			s.thought.WriteString(str(d["reasoning_content"]))
			for _, t := range list(d["tool_calls"]) {
				s.tool.WriteString(str(object(object(t)["function"])["arguments"]))
			}
			if reason := str(ch["finish_reason"]); reason != "" {
				if reason != "stop" && reason != "tool_calls" {
					return fmt.Errorf("non-success chat finish: %s", reason)
				}
				s.terminal = true
				s.o.Terminal = reason
			}
		}
		s.usage(object(m["usage"]), "prompt_tokens", "completion_tokens")
	case "responses", "ws":
		t := str(m["type"])
		switch t {
		case "response.output_text.delta":
			delta := str(m["delta"])
			s.content(delta)
			if s.textParts == nil {
				s.textParts = make(map[string]*strings.Builder)
			}
			key := responsesTextPartKey(m)
			if s.textParts[key] == nil {
				s.textParts[key] = &strings.Builder{}
			}
			s.textParts[key].WriteString(delta)
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			s.thought.WriteString(str(m["delta"]))
		case "response.function_call_arguments.delta":
			s.tool.WriteString(str(m["delta"]))
		case "response.output_text.done":
			if s.completedTextParts == nil {
				s.completedTextParts = make(map[string]string)
			}
			s.completedTextParts[responsesTextPartKey(m)] = str(m["text"])
		case "response.failed", "response.incomplete", "error":
			return fmt.Errorf("non-success responses event: %s", t)
		case "response.completed":
			r := object(m["response"])
			if str(r["status"]) != "completed" {
				return errors.New("response.completed lacks completed status")
			}
			s.terminal = true
			s.done = true
			s.o.Terminal = t
			s.usage(object(r["usage"]), "input_tokens", "output_tokens")
		}
	case "claude":
		switch str(m["type"]) {
		case "message_start":
			r := object(m["message"])
			s.o.MockResponseID = str(r["id"])
			s.usage(object(r["usage"]), "input_tokens", "output_tokens")
		case "content_block_delta":
			d := object(m["delta"])
			switch str(d["type"]) {
			case "text_delta":
				s.content(str(d["text"]))
			case "thinking_delta":
				s.thought.WriteString(str(d["thinking"]))
			case "input_json_delta":
				s.tool.WriteString(str(d["partial_json"]))
			}
		case "message_delta":
			d := object(m["delta"])
			reason := str(d["stop_reason"])
			if reason != "" {
				if reason != "end_turn" && reason != "tool_use" {
					return fmt.Errorf("non-success claude finish: %s", reason)
				}
				s.terminal = true
				s.o.Terminal = reason
			}
			s.usage(object(m["usage"]), "input_tokens", "output_tokens")
		case "message_stop":
			s.done = true
		}
	case "gemini":
		if id := str(m["responseId"]); id != "" {
			s.o.MockResponseID = id
		}
		for _, raw := range list(m["candidates"]) {
			c := object(raw)
			for _, part := range list(object(c["content"])["parts"]) {
				p := object(part)
				if p["thought"] == true {
					s.thought.WriteString(str(p["text"]))
				} else {
					s.content(str(p["text"]))
				}
				if f := object(p["functionCall"]); f != nil {
					args, _ := json.Marshal(f["args"])
					s.tool.Write(args)
				}
			}
			if reason := str(c["finishReason"]); reason != "" {
				if reason != "STOP" {
					return fmt.Errorf("non-success gemini finish: %s", reason)
				}
				s.terminal = true
				s.done = true
				s.o.Terminal = reason
			}
		}
		s.usage(object(m["usageMetadata"]), "promptTokenCount", "candidatesTokenCount")
	}
	return nil
}

func (s *streamState) finish(w workload) error {
	s.o.CompleteMS = float64(time.Since(s.start)) / float64(time.Millisecond)
	s.o.TextBytes = s.text.Len()
	s.o.ThoughtBytes = s.thought.Len()
	s.o.ToolBytes = s.tool.Len()
	if !s.terminal || !s.done {
		return errors.New("truncated stream: successful terminal and protocol completion required")
	}
	if s.text.String() != w.Text {
		return fmt.Errorf("text mismatch: got %d bytes expected %d", s.text.Len(), len(w.Text))
	}
	for key, text := range s.completedTextParts {
		part := s.textParts[key]
		if (part == nil && text != "") || (part != nil && part.String() != text) {
			return fmt.Errorf("terminal text part %s differs from its own deltas", key)
		}
	}
	if s.thought.String() != w.Thought {
		return fmt.Errorf("thinking mismatch: got %d bytes expected %d", s.thought.Len(), len(w.Thought))
	}
	if w.Tool != "" {
		var got, want any
		if json.Unmarshal([]byte(s.tool.String()), &got) != nil || json.Unmarshal([]byte(w.Tool), &want) != nil {
			return errors.New("invalid or missing fragmented tool arguments")
		}
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(want)
		if string(a) != string(b) {
			return errors.New("tool arguments mismatch")
		}
	} else if s.tool.Len() != 0 {
		return errors.New("unexpected tool arguments")
	}
	if s.o.InputTokens != w.InputTokens || s.o.OutputTokens != w.OutputTokens {
		return fmt.Errorf("usage mismatch: got %d/%d expected %d/%d", s.o.InputTokens, s.o.OutputTokens, w.InputTokens, w.OutputTokens)
	}
	return nil
}

func responsesTextPartKey(event map[string]any) string {
	return fmt.Sprintf("%s/%d/%d", str(event["item_id"]), num(event["output_index"]), num(event["content_index"]))
}

func readSSE(r io.Reader, accept func([]byte) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 8<<20)
	var data []string
	dispatch := func() error {
		if len(data) == 0 {
			return nil
		}
		err := accept([]byte(strings.Join(data, "\n")))
		data = nil
		return err
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return dispatch()
}
