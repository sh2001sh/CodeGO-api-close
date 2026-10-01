package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

type responseStream struct {
	body     io.ReadCloser
	reader   *sse.Reader
	req      *gateway.Request
	native   bool
	ended    bool
	done     bool
	chat     *chatState
	finished map[int]bool
	semantic bool
}

func (s *responseStream) Next() (gateway.Event, error) {
	if s.done {
		return gateway.Event{}, io.EOF
	}
	if s.ended {
		s.done = true
		if s.req.Stream && !s.native {
			return gateway.Event{Kind: gateway.EventDone}, nil
		}
		return gateway.Event{}, io.EOF
	}
	data, event, err, ret := s.readData()
	if ret {
		return event, err
	}
	response, event, err, ret := s.decodeResponse(data)
	if ret {
		return event, err
	}
	s.trackCandidateFinish(response)
	usage := usageFrom(response.Usage)
	s.semantic = semanticResponse(response, s.native)
	if s.native {
		return gateway.Event{Kind: gateway.EventData, Payload: data, Usage: usage, TextBytes: responseTextBytes(response)}, nil
	}
	return s.chat.event(response, usage, s.req.Stream)
}

// readData reads the next raw response payload from the SSE reader or (for
// a single JSON body) the whole body. ret is true when Next should return
// (event, err) immediately; when ret is false, data holds the payload to
// decode (and recursing into Next after exhausting an SSE stream is handled
// here directly).
func (s *responseStream) readData() (data []byte, event gateway.Event, err error, ret bool) {
	if s.reader == nil {
		s.ended = true
		data, err = io.ReadAll(io.LimitReader(s.body, maxJSONBody+1))
		if err != nil {
			return nil, gateway.Event{}, err, true
		}
		if len(data) > maxJSONBody {
			return nil, gateway.Event{}, fmt.Errorf("gemini: response exceeds body limit"), true
		}
		return data, gateway.Event{}, nil, false
	}
	ev, readErr := s.reader.Next()
	if readErr == io.EOF {
		for _, finished := range s.finished {
			if !finished {
				s.done = true
				return nil, gateway.Event{}, io.ErrUnexpectedEOF, true
			}
		}
		s.ended = true
		event, err = s.Next()
		return nil, event, err, true
	}
	if readErr != nil {
		return nil, gateway.Event{}, readErr, true
	}
	return ev.Data, gateway.Event{}, nil, false
}

// decodeResponse parses and validates one native response payload. ret is
// true when Next should return (event, err) immediately (malformed JSON,
// upstream error, or a blocked prompt).
func (s *responseStream) decodeResponse(data []byte) (response generateResponse, event gateway.Event, err error, ret bool) {
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '{' {
		return response, gateway.Event{}, fmt.Errorf("gemini: upstream response must be a JSON object"), true
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return response, gateway.Event{}, fmt.Errorf("gemini: invalid upstream JSON: %w", err), true
	}
	if response.Error != nil {
		status := response.Error.Code
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		return response, gateway.Event{Kind: gateway.EventError, Err: &gateway.UpstreamError{Status: status,
			Type: "upstream_error", Code: response.Error.Status, Message: response.Error.Message}}, nil, true
	}
	if response.Feedback != nil && response.Feedback.BlockReason != "" {
		return response, gateway.Event{Kind: gateway.EventError, Err: &gateway.UpstreamError{Status: http.StatusBadRequest,
			Type: "invalid_request_error", Code: "content_filter", Message: "Gemini blocked this prompt: " + response.Feedback.BlockReason}}, nil, true
	}
	return response, gateway.Event{}, nil, false
}

// trackCandidateFinish records, per streamed candidate index, whether a
// finish reason has been seen yet; used to detect a truncated SSE stream.
func (s *responseStream) trackCandidateFinish(response generateResponse) {
	if s.reader == nil {
		return
	}
	if s.finished == nil {
		s.finished = make(map[int]bool)
	}
	for position, candidate := range response.Candidates {
		index := candidateIndex(candidate, position)
		s.finished[index] = s.finished[index] || candidate.FinishReason != ""
	}
}

func candidateIndex(candidate candidate, position int) int {
	if candidate.Index == 0 && position != 0 {
		return position
	}
	return candidate.Index
}

func (s *responseStream) Close() error { return s.body.Close() }

func usageFrom(metadata *usageMetadata) *gateway.Usage {
	if metadata == nil {
		return nil
	}
	u := &gateway.Usage{PromptTokens: metadata.Prompt + metadata.ToolInput,
		CompletionTokens: metadata.Output + metadata.Thoughts, CachedTokens: metadata.Cached}
	for _, detail := range metadata.PromptDetails {
		switch detail.Modality {
		case "IMAGE":
			u.ImageInputTokens += detail.Count
		case "AUDIO":
			u.AudioInputTokens += detail.Count
		}
	}
	for _, detail := range metadata.OutputDetails {
		switch detail.Modality {
		case "IMAGE":
			u.ImageOutputTokens += detail.Count
		case "AUDIO":
			u.AudioOutputTokens += detail.Count
		}
	}
	return u
}

func responseTextBytes(response generateResponse) int {
	total := 0
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			total += len(part.Text)
			if part.FunctionCall != nil {
				total += len(part.FunctionCall.Name) + len(part.FunctionCall.Args)
			}
		}
	}
	return total
}
