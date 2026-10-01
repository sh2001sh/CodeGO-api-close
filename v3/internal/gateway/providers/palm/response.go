package palm

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

const maxResponseBody = 64 << 20

type nativeResponse struct {
	Candidates []nativeMessage `json:"candidates"`
	Filters    []struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	} `json:"filters"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

type chatChoice struct {
	Index        int          `json:"index"`
	Message      *chatMessage `json:"message,omitempty"`
	Delta        *chatMessage `json:"delta,omitempty"`
	FinishReason string       `json:"finish_reason"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
}

type responseStream struct {
	req      *gateway.Request
	response *http.Response
	body     io.ReadCloser
	phase    uint8
}

func (s *responseStream) Next() (gateway.Event, error) {
	if s.phase == 2 {
		return gateway.Event{}, io.EOF
	}
	if s.phase == 1 {
		s.phase = 2
		return gateway.Event{Kind: gateway.EventDone}, nil
	}
	s.phase = 2 // Read and parse failures cannot be mistaken for a clean terminal.
	if s.response.Request != nil {
		if err := s.response.Request.Context().Err(); err != nil {
			return gateway.Event{}, err
		}
	}
	root, err := s.readBody()
	if err != nil {
		return gateway.Event{}, err
	}
	if root.Error != nil {
		return errorEvent(root.Error), nil
	}
	if len(root.Candidates) == 0 {
		if len(root.Filters) > 0 {
			return gateway.Event{Kind: gateway.EventError, Err: upstream("content_filter", "PaLM filtered all candidates")}, nil
		}
		return gateway.Event{Kind: gateway.EventError, Err: upstream("empty_response", "PaLM returned no candidates")}, nil
	}
	out, textBytes, errEvent := s.buildResponse(root.Candidates)
	if errEvent != nil {
		return *errEvent, nil
	}
	payload, err := json.Marshal(out)
	if err != nil {
		return gateway.Event{}, err
	}
	s.phase = 1
	// generateMessage returns no native counts. The gateway owns fallback estimates.
	return gateway.Event{Kind: gateway.EventData, Payload: payload, TextBytes: textBytes}, nil
}

// readBody drains and decodes the upstream response body, enforcing the
// size limit before JSON decoding.
func (s *responseStream) readBody() (nativeResponse, error) {
	body, err := io.ReadAll(io.LimitReader(s.body, maxResponseBody+1))
	if err != nil {
		return nativeResponse{}, err
	}
	if len(body) > maxResponseBody {
		return nativeResponse{}, errors.New("PaLM response exceeds size limit")
	}
	var root nativeResponse
	if len(body) == 0 || json.Unmarshal(body, &root) != nil {
		return nativeResponse{}, errors.New("PaLM returned invalid JSON")
	}
	return root, nil
}

func errorEvent(upstreamErr *struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}) gateway.Event {
	status := upstreamErr.Code
	if status < 400 || status > 599 {
		status = http.StatusBadGateway
	}
	kind := upstreamErr.Status
	if kind == "" {
		kind = "upstream_error"
	}
	message := upstreamErr.Message
	if message == "" {
		message = "PaLM reported an error"
	}
	return gateway.Event{Kind: gateway.EventError, Err: &gateway.UpstreamError{
		Status: status, Type: kind, Code: strconv.Itoa(upstreamErr.Code), Message: message,
	}}
}

// buildResponse converts PaLM candidates into the OpenAI-shaped chat
// response. errEvent is non-nil (and out/textBytes are zero) if a candidate
// is invalid and Next must return an error immediately.
func (s *responseStream) buildResponse(candidates []nativeMessage) (out chatResponse, textBytes int, errEvent *gateway.Event) {
	id := s.req.ID
	if id == "" {
		id = "chatcmpl-palm"
	}
	created := s.req.Received.Unix()
	if s.req.Received.IsZero() {
		created = time.Now().Unix()
	}
	out = chatResponse{ID: id, Object: "chat.completion", Created: created, Model: s.req.Model}
	if s.req.Stream {
		out.Object = "chat.completion.chunk"
	}
	for i, candidate := range candidates {
		if candidate.Content == "" {
			ev := gateway.Event{Kind: gateway.EventError, Err: upstream("empty_response", "PaLM returned an empty candidate")}
			return chatResponse{}, 0, &ev
		}
		message := &chatMessage{Role: "assistant", Content: candidate.Content}
		choice := chatChoice{Index: i, FinishReason: "stop"}
		if s.req.Stream {
			choice.Delta = message
		} else {
			choice.Message = message
		}
		out.Choices = append(out.Choices, choice)
		textBytes += len(candidate.Content)
	}
	return out, textBytes, nil
}

func (s *responseStream) Close() error {
	s.phase = 2
	return s.body.Close()
}
