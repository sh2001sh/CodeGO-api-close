package auxiliary

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type streamAdapter interface {
	DecodeStream(*gateway.Request, gateway.Target, Input, *http.Response) gateway.EventStream
}

// eventReader lets the common SSE relay retain its failover/drain decisions
// while native adapters convert provider events into the client protocol.
type eventReader struct {
	events  gateway.EventStream
	pending []byte
	done    bool
}

func (r *eventReader) Close() error { return r.events.Close() }

func (r *eventReader) Read(buffer []byte) (int, error) {
	for len(r.pending) == 0 {
		if r.done {
			return 0, io.EOF
		}
		event, err := r.events.Next()
		if err != nil {
			return 0, err
		}
		switch event.Kind {
		case gateway.EventData:
			r.pending = sseFrame(event.Name, event.Payload)
			if event.Usage != nil {
				r.pending = append(r.pending, sseFrame("usage", usageJSON(*event.Usage))...)
			}
		case gateway.EventUsage:
			if event.Usage != nil {
				r.pending = sseFrame("usage", usageJSON(*event.Usage))
			}
		case gateway.EventDone:
			r.pending, r.done = sseFrame("", []byte("[DONE]")), true
		case gateway.EventError:
			payload, _ := json.Marshal(map[string]any{"error": map[string]string{"code": "upstream_stream_error", "message": "upstream stream reported an error"}})
			r.pending, r.done = sseFrame("error", payload), true
			if event.Usage != nil {
				r.pending = append(sseFrame("usage", usageJSON(*event.Usage)), r.pending...)
			}
		}
	}
	n := copy(buffer, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func sseFrame(name string, payload []byte) []byte {
	frame := make([]byte, 0, len(payload)+32)
	if name != "" {
		frame = append(frame, []byte("event: "+name+"\n")...)
	}
	frame = append(frame, []byte("data: ")...)
	frame = append(frame, payload...)
	return append(frame, '\n', '\n')
}

func usageJSON(usage gateway.Usage) []byte {
	body, _ := json.Marshal(map[string]any{"usage": map[string]any{
		"prompt_tokens": usage.PromptTokens, "completion_tokens": usage.CompletionTokens,
		"estimated": usage.Estimated, "image_count": usage.ImageCount, "audio_characters": usage.AudioCharacters,
		"audio_duration_micros": usage.AudioDurationMicros, "video_duration_micros": usage.VideoDurationMicros,
		"cache_creation_input_tokens": usage.CacheWriteTokens,
		"prompt_tokens_details":       map[string]int64{"cached_tokens": usage.CachedTokens, "audio_tokens": usage.AudioInputTokens, "image_tokens": usage.ImageInputTokens},
		"completion_tokens_details":   map[string]int64{"audio_tokens": usage.AudioOutputTokens, "image_tokens": usage.ImageOutputTokens},
	}})
	return body
}
