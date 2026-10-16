package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tiktoken-go/tokenizer/codec"
)

type profiledWorkload struct {
	SkipWarmup                                           bool
	Protocol, Model                                      string
	InputTokens, OutputTokens, CachedTokens, ChunkTokens int
	Timeout                                              time.Duration
	DropEvery                                            int64
}

func (p profiledWorkload) validate() error {
	if (p.Protocol != "chat" && p.Protocol != "responses") || p.Model == "" ||
		p.InputTokens < 0 || p.InputTokens > 250000 || p.OutputTokens < 1 || p.OutputTokens > 16000 ||
		p.CachedTokens < 0 || p.CachedTokens > p.InputTokens || p.ChunkTokens < 1 || p.Timeout <= 0 || p.DropEvery < 0 {
		return fmt.Errorf("invalid profiled workload")
	}
	return nil
}

func (p profiledWorkload) requestBody() ([]byte, string, int, int, error) {
	// Use mixed source code and documentation, rather than merely claiming a
	// large prompt in upstream usage. These aliases use cl100k in this gateway.
	enc := codec.NewCl100kBase()
	pattern := "# Request processing\n```go\nfunc serveRequest(ctx context.Context, accountID string) error {\n\tif err := reserveBalance(ctx, accountID); err != nil { return err }\n\treturn streamResponse(ctx)\n}\n```\nThe client expects ordered output and an atomic settlement.\n"
	per, err := enc.Count(pattern)
	if err != nil {
		return nil, "", 0, 0, err
	}
	ids, _, err := enc.Encode(strings.Repeat(pattern, p.InputTokens/per+2))
	if err != nil {
		return nil, "", 0, 0, err
	}
	if len(ids) < p.InputTokens {
		return nil, "", 0, 0, fmt.Errorf("prompt construction produced too few tokens")
	}
	prompt, err := enc.Decode(ids[:p.InputTokens])
	if err != nil {
		return nil, "", 0, 0, err
	}
	verified, err := enc.Count(prompt)
	if err != nil {
		return nil, "", 0, 0, err
	}
	outputCount, err := enc.Count(strings.Repeat(" token", p.OutputTokens))
	if err != nil {
		return nil, "", 0, 0, err
	}
	if verified != p.InputTokens || outputCount != p.OutputTokens {
		return nil, "", 0, 0, fmt.Errorf("fixture token mismatch: input=%d output=%d", verified, outputCount)
	}
	request := map[string]any{"model": p.Model, "stream": true}
	path := "/v1/chat/completions"
	if p.Protocol == "responses" {
		path = "/v1/responses"
		request["input"] = []any{map[string]any{"role": "user", "content": []any{map[string]string{"type": "input_text", "text": prompt}}}}
		request["max_output_tokens"] = p.OutputTokens
		request["store"] = false
	} else {
		request["messages"] = []map[string]string{{"role": "user", "content": prompt}}
		request["max_tokens"] = p.OutputTokens
		request["stream_options"] = map[string]bool{"include_usage": true}
	}
	body, err := json.Marshal(request)
	return body, path, verified, outputCount, err
}

func returnProfiledStream(w http.ResponseWriter, r *http.Request, first, tail time.Duration, p profiledWorkload, call int64) {
	w.Header().Set("Content-Type", "text/event-stream")
	write := func(event map[string]any) bool {
		raw, err := json.Marshal(event)
		if err != nil {
			return false
		}
		if _, err = fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
			return false
		}
		w.(http.Flusher).Flush()
		return true
	}
	responseID := fmt.Sprintf("resp_fixture_%d", call)
	if p.Protocol == "responses" {
		if !write(map[string]any{"type": "response.created", "response": map[string]any{"id": responseID, "object": "response", "model": p.Model, "status": "in_progress"}}) {
			return
		}
	}
	if !waitContext(r.Context(), first) {
		return
	}
	// Stable standard fields survive a gateway that formats/paces each frame.
	// The extra timestamp field alone may be removed by DTO serialization.
	sentAt := time.Now().UnixNano()
	if p.Protocol == "chat" {
		responseID = fmt.Sprintf("resp_fixture_%d_%d", call, sentAt)
	}
	itemID := fmt.Sprintf("msg_fixture_%d", sentAt)
	if p.Protocol == "responses" {
		if !write(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": itemID, "type": "message", "role": "assistant", "content": []any{}}}) {
			return
		}
		if !write(map[string]any{"type": "response.content_part.added", "output_index": 0, "content_index": 0, "item_id": itemID, "part": map[string]any{"type": "output_text", "text": ""}}) {
			return
		}
	}
	chunks := (p.OutputTokens + p.ChunkTokens - 1) / p.ChunkTokens
	start := time.Now()
	for i, emitted := 0, 0; emitted < p.OutputTokens; i++ {
		if i > 0 && !waitContext(r.Context(), max(0, time.Until(start.Add(time.Duration(i)*tail/time.Duration(max(1, chunks-1)))))) {
			return
		}
		if p.DropEvery > 0 && call%p.DropEvery == 0 && i >= max(1, chunks/2) {
			return
		}
		n := min(p.ChunkTokens, p.OutputTokens-emitted)
		text := strings.Repeat(" token", n)
		var event map[string]any
		if p.Protocol == "responses" {
			event = map[string]any{"type": "response.output_text.delta", "item_id": itemID, "output_index": 0, "content_index": 0, "delta": text}
		} else {
			event = map[string]any{"id": responseID, "model": p.Model, "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]string{"content": text}, "finish_reason": nil}}}
		}
		event["fixture_sent_unix_nano"] = time.Now().UnixNano()
		if !write(event) {
			return
		}
		emitted += n
	}
	usage := map[string]any{"input_tokens": p.InputTokens, "output_tokens": p.OutputTokens, "total_tokens": p.InputTokens + p.OutputTokens, "input_tokens_details": map[string]int{"cached_tokens": p.CachedTokens}}
	if p.Protocol == "responses" {
		output := []any{map[string]any{"type": "message", "id": itemID, "role": "assistant", "status": "completed", "content": []any{map[string]string{"type": "output_text", "text": strings.Repeat(" token", p.OutputTokens)}}}}
		write(map[string]any{"type": "response.completed", "response": map[string]any{"id": responseID, "object": "response", "model": p.Model, "status": "completed", "output": output, "usage": usage}})
	} else {
		if !write(map[string]any{"id": responseID, "model": p.Model, "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]string{}, "finish_reason": "stop"}}}) {
			return
		}
		if !write(map[string]any{"id": responseID, "model": p.Model, "object": "chat.completion.chunk", "choices": []any{}, "usage": map[string]any{"prompt_tokens": p.InputTokens, "completion_tokens": p.OutputTokens, "total_tokens": p.InputTokens + p.OutputTokens, "prompt_tokens_details": map[string]int{"cached_tokens": p.CachedTokens}}}) {
			return
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}
}

func readProfiledStream(body io.Reader, started time.Time, p profiledWorkload) observation {
	obs := observation{}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	var content strings.Builder
	var last time.Time
	var done, usageOK bool
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if raw == "[DONE]" {
			done = true
			continue
		}
		var event struct {
			Type, Delta string
			ID          string `json:"id"`
			ItemID      string `json:"item_id"`
			Choices     []struct{ Delta struct{ Content string } }
			Sent        int64 `json:"fixture_sent_unix_nano"`
			Usage       *struct {
				Prompt     int `json:"prompt_tokens"`
				Completion int `json:"completion_tokens"`
				Details    struct {
					Cached int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
			}
			Response *struct {
				Usage *struct {
					Input   int `json:"input_tokens"`
					Output  int `json:"output_tokens"`
					Details struct {
						Cached int `json:"cached_tokens"`
					} `json:"input_tokens_details"`
				}
			}
			Error json.RawMessage
		}
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			obs.Error = "invalid SSE JSON"
			continue
		}
		if len(event.Error) > 0 && string(event.Error) != "null" {
			obs.Error = string(event.Error)
		}
		if event.Type == "response.failed" || event.Type == "response.incomplete" || event.Type == "error" {
			obs.Error = event.Type
		}
		text := ""
		if p.Protocol == "responses" {
			if event.Type == "response.output_text.delta" {
				text = event.Delta
			}
			if event.Type == "response.completed" {
				done = true
			}
			if event.Response != nil && event.Response.Usage != nil {
				u := event.Response.Usage
				usageOK = u.Input == p.InputTokens && u.Output == p.OutputTokens && u.Details.Cached == p.CachedTokens
			}
		} else {
			for _, choice := range event.Choices {
				text += choice.Delta.Content
			}
			if u := event.Usage; u != nil {
				usageOK = u.Prompt == p.InputTokens && u.Completion == p.OutputTokens && u.Details.Cached == p.CachedTokens
			}
		}
		if text != "" {
			now := time.Now()
			if obs.First == 0 {
				obs.First = float64(now.Sub(started).Microseconds()) / 1000
				if event.Sent == 0 {
					id := event.ID
					if p.Protocol == "responses" {
						id = event.ItemID
					}
					if index := strings.LastIndexByte(id, '_'); index >= 0 {
						event.Sent, _ = strconv.ParseInt(id[index+1:], 10, 64)
					}
				}
				if event.Sent > 0 {
					obs.DeliveryMeasured = true
					obs.Delivery = float64(now.Sub(time.Unix(0, event.Sent)).Microseconds()) / 1000
				}
			}
			if !last.IsZero() {
				obs.MaxGap = max(obs.MaxGap, float64(now.Sub(last).Microseconds())/1000)
			}
			last = now
			obs.Chunks++
			obs.OutputBytes += len(text)
			content.WriteString(text)
		}
	}
	if err := scanner.Err(); err != nil {
		obs.Error = err.Error()
	}
	obs.OK = done && usageOK && content.String() == strings.Repeat(" token", p.OutputTokens) && obs.Error == ""
	if !obs.OK && obs.Error == "" {
		obs.Error = fmt.Sprintf("incomplete stream: terminal=%v usage=%v content_bytes=%d", done, usageOK, content.Len())
	}
	return obs
}
