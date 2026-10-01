package gateway

import "encoding/json"

// SampleRecorder selects requests and enqueues their settled, bounded audit
// samples. Record must not wait for storage or for space in a full queue.
type SampleRecorder interface {
	ShouldSample(id string) bool
	MaxBytes() int
	Record(req *Request, out Outcome, response json.RawMessage)
}

// ResponseSample owns copies of actual adapter payloads. Its memory and encoded
// size stay bounded even when an upstream emits an indefinitely long stream.
type ResponseSample struct {
	max       int
	bytes     int
	events    []json.RawMessage
	truncated bool
}

const sampleEnvelopeBytes = len(`{"events":[],"truncated":false}`)

func NewResponseSample(maxBytes int) *ResponseSample {
	return &ResponseSample{max: max(maxBytes, sampleEnvelopeBytes), bytes: sampleEnvelopeBytes}
}

// Add captures only data and in-band errors; accounting is recorded separately
// after finalization. Invalid or oversized payloads produce an explicit marker.
func (s *ResponseSample) Add(event Event) {
	if s == nil || s.truncated || (event.Kind != EventData && event.Kind != EventError) {
		return
	}
	if len(event.Payload) == 0 {
		return
	}
	if len(event.Payload)+len(event.Name)+s.bytes > s.max || !json.Valid(event.Payload) {
		s.truncated = true
		return
	}
	kind := "data"
	if event.Kind == EventError {
		kind = "error"
	}
	b, err := json.Marshal(struct {
		Kind    string          `json:"kind"`
		Name    string          `json:"name,omitempty"`
		Payload json.RawMessage `json:"payload"`
	}{kind, event.Name, event.Payload})
	separator := 0
	if len(s.events) > 0 {
		separator = 1
	}
	if err != nil || s.bytes+len(b)+separator > s.max {
		s.truncated = true
		return
	}
	s.events = append(s.events, b)
	s.bytes += len(b) + separator
}

// JSON returns an independent JSON object, allowing audit redaction to traverse
// payloads as objects rather than treating SSE data as opaque escaped strings.
func (s *ResponseSample) JSON() json.RawMessage {
	if s == nil {
		return json.RawMessage(`{"events":[],"truncated":false}`)
	}
	b := make([]byte, 0, s.bytes)
	b = append(b, `{"events":[`...)
	for i, event := range s.events {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, event...)
	}
	if s.truncated {
		return append(b, `],"truncated":true}`...)
	}
	return append(b, `],"truncated":false}`...)
}
