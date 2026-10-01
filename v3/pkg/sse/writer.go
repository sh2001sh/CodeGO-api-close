package sse

import (
	"bytes"
	"net/http"
)

// Writer frames events for a client and flushes after each one.
// It is not safe for concurrent use; the stream driver owns it.
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
	buf     []byte
}

// NewWriter sets SSE headers on w. Headers are sent on the first write, so
// callers can still fail over before the first event (plan §4 first-byte gate).
func NewWriter(w http.ResponseWriter) *Writer {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // disable Nginx proxy buffering
	flusher, _ := w.(http.Flusher)
	return &Writer{w: w, flusher: flusher, buf: make([]byte, 0, 4<<10)}
}

// WriteEvent writes one event. name may be empty. Multi-line data is split
// into several "data:" lines as the SSE format requires.
func (sw *Writer) WriteEvent(name string, data []byte) error {
	b := sw.buf[:0]
	if name != "" {
		b = append(b, "event: "...)
		b = append(b, name...)
		b = append(b, '\n')
	}
	for {
		line, rest, found := bytes.Cut(data, []byte{'\n'})
		b = append(b, "data: "...)
		b = append(b, line...)
		b = append(b, '\n')
		if !found {
			break
		}
		data = rest
	}
	b = append(b, '\n')
	sw.buf = b
	return sw.write(b)
}

// WriteComment writes a keep-alive comment that clients ignore.
func (sw *Writer) WriteComment(text string) error {
	b := append(sw.buf[:0], ": "...)
	b = append(b, text...)
	b = append(b, "\n\n"...)
	sw.buf = b
	return sw.write(b)
}

func (sw *Writer) write(b []byte) error {
	if _, err := sw.w.Write(b); err != nil {
		return err
	}
	if sw.flusher != nil {
		sw.flusher.Flush()
	}
	return nil
}
