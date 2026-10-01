package sse

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func readAll(t *testing.T, input string) ([]Event, error) {
	t.Helper()
	r := NewReader(strings.NewReader(input), 0)
	var events []Event
	for {
		ev, err := r.Next()
		if err != nil {
			return events, err
		}
		// Copy because slices are reused by the next call.
		events = append(events, Event{
			Name: append([]byte(nil), ev.Name...),
			ID:   append([]byte(nil), ev.ID...),
			Data: append([]byte(nil), ev.Data...),
		})
	}
}

func TestReaderParsesOpenAIAndClaudeStyles(t *testing.T) {
	input := ": keep-alive\n\n" +
		"data: {\"a\":1}\n\n" +
		"event: message_delta\r\ndata: {\"b\":2}\r\n\r\n" +
		"data: line1\ndata: line2\n\n" +
		"data: [DONE]\n\n"
	events, err := readAll(t, input)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("want io.EOF, got %v", err)
	}
	want := []struct{ name, data string }{
		{"", `{"a":1}`},
		{"message_delta", `{"b":2}`},
		{"", "line1\nline2"},
		{"", "[DONE]"},
	}
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d", len(events), len(want))
	}
	for i, w := range want {
		if string(events[i].Name) != w.name || string(events[i].Data) != w.data {
			t.Errorf("event %d = (%q, %q); want (%q, %q)", i, events[i].Name, events[i].Data, w.name, w.data)
		}
	}
}

func TestReaderReportsTruncatedEvent(t *testing.T) {
	events, err := readAll(t, "data: {\"a\":1}\n\ndata: {\"partial\"")
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("want io.ErrUnexpectedEOF, got %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d complete events, want 1", len(events))
	}
}

func TestReaderEnforcesMaxSize(t *testing.T) {
	r := NewReader(strings.NewReader("data: "+strings.Repeat("x", 100)+"\n\n"), 50)
	if _, err := r.Next(); !errors.Is(err, ErrEventTooLarge) {
		t.Fatalf("want ErrEventTooLarge, got %v", err)
	}
}

func TestReaderHandlesLinesLongerThanBuffer(t *testing.T) {
	big := strings.Repeat("y", 200<<10) // larger than the 64 KiB bufio buffer
	events, err := readAll(t, "data: "+big+"\n\n")
	if !errors.Is(err, io.EOF) || len(events) != 1 || string(events[0].Data) != big {
		t.Fatalf("long line not parsed: events=%d err=%v", len(events), err)
	}
}

func TestWriterRoundTrip(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewWriter(rec)
	if err := w.WriteComment("ping"); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteEvent("delta", []byte("a\nb")); err != nil {
		t.Fatal(err)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type = %q", got)
	}
	if !rec.Flushed {
		t.Fatal("writer did not flush")
	}
	events, err := readAll(t, rec.Body.String())
	if !errors.Is(err, io.EOF) || len(events) != 1 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
	if string(events[0].Name) != "delta" || string(events[0].Data) != "a\nb" {
		t.Fatalf("round trip = (%q, %q)", events[0].Name, events[0].Data)
	}
}
