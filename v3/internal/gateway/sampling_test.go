package gateway

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestResponseSampleOwnsActualDataAndErrorPayloads(t *testing.T) {
	s := NewResponseSample(2048)
	payload := []byte(`{"text":"original","nested":{"api_key":"secret"}}`)
	s.Add(Event{Kind: EventData, Name: "message", Payload: payload})
	s.Add(Event{Kind: EventUsage, Payload: []byte(`{"usage":"ignored"}`)})
	s.Add(Event{Kind: EventError, Payload: []byte(`{"error":{"code":"overloaded"}}`)})
	copy(payload, bytes.Repeat([]byte("x"), len(payload)))
	var result struct {
		Events []struct {
			Kind    string
			Name    string
			Payload map[string]any
		}
		Truncated bool
	}
	if err := json.Unmarshal(s.JSON(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 || result.Truncated || result.Events[0].Payload["text"] != "original" || result.Events[0].Name != "message" {
		t.Fatalf("lost actual payloads: %+v", result)
	}
	if result.Events[1].Kind != "error" || result.Events[1].Payload["error"].(map[string]any)["code"] != "overloaded" {
		t.Fatalf("lost actual upstream error: %+v", result.Events[1])
	}
	encoded := s.JSON()
	copy(encoded, bytes.Repeat([]byte("x"), len(encoded)))
	if !json.Valid(s.JSON()) {
		t.Fatal("caller mutation changed the captured sample")
	}
}

func TestResponseSampleBoundsLongStreamAndRejectsInvalidJSON(t *testing.T) {
	for _, payload := range []string{`{"delta":"` + strings.Repeat("x", 600) + `"}`, `not JSON`} {
		s := NewResponseSample(256)
		s.Add(Event{Kind: EventData, Payload: []byte(`{"delta":"retained"}`)})
		for range 10_000 {
			s.Add(Event{Kind: EventData, Payload: []byte(payload)})
		}
		b := s.JSON()
		if len(b) > 256 || !json.Valid(b) || !bytes.Contains(b, []byte(`"truncated":true`)) || !bytes.Contains(b, []byte("retained")) {
			t.Fatalf("unbounded or invalid sample (%d): %s", len(b), b)
		}
		if s.bytes > 256 || len(s.events) != 1 {
			t.Fatal("long stream continued accumulating after truncation")
		}
	}
}

func TestResponseSampleWrapperAndEventNameCountTowardLimit(t *testing.T) {
	s := NewResponseSample(80)
	s.Add(Event{Kind: EventData, Name: strings.Repeat("n", 80), Payload: []byte(`{}`)})
	if len(s.JSON()) > 80 || !bytes.Contains(s.JSON(), []byte(`"truncated":true`)) {
		t.Fatalf("event metadata escaped bound: %s", s.JSON())
	}
	var absent *ResponseSample
	absent.Add(Event{Kind: EventData})
	if !json.Valid(absent.JSON()) {
		t.Fatal("nil sample is not valid JSON")
	}
}
