// Package replay records upstream HTTP responses with chunk timing and plays
// them back, so provider and terminal-state tests run against real traffic
// shapes without network access (plan §4, borrowed from CLIProxyAPI).
package replay

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// Chunk is one body read as it arrived from the upstream.
type Chunk struct {
	DelayMs int64  `json:"delay_ms"` // since the previous chunk (or headers)
	Data    string `json:"data"`
}

// Recording is one upstream response. Truncated marks a body that ended with
// an error instead of io.EOF, so playback can reproduce the drop.
type Recording struct {
	Status    int         `json:"status"`
	Header    http.Header `json:"header"`
	Chunks    []Chunk     `json:"chunks"`
	Truncated bool        `json:"truncated,omitempty"`
}

// redactedHeaders never reach a recording file.
var redactedHeaders = []string{"Set-Cookie", "Authorization", "X-Api-Key", "Openai-Organization"}

// Recorder is an http.RoundTripper that tees each response into a Recording.
type Recorder struct {
	Base http.RoundTripper

	mu         sync.Mutex
	recordings []*Recording
}

// RoundTrip performs the request and wraps the body so reads are recorded.
func (r *Recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	base := r.Base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	rec := &Recording{Status: resp.StatusCode, Header: resp.Header.Clone()}
	for _, h := range redactedHeaders {
		rec.Header.Del(h)
	}
	r.mu.Lock()
	r.recordings = append(r.recordings, rec)
	r.mu.Unlock()
	resp.Body = &teeBody{rc: resp.Body, rec: rec, last: time.Now()}
	return resp, nil
}

// Recordings returns what has been captured so far. Read it only after the
// response bodies are closed.
func (r *Recorder) Recordings() []*Recording {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*Recording(nil), r.recordings...)
}

type teeBody struct {
	rc   io.ReadCloser
	rec  *Recording
	last time.Time
}

func (t *teeBody) Read(p []byte) (int, error) {
	n, err := t.rc.Read(p)
	if n > 0 {
		now := time.Now()
		t.rec.Chunks = append(t.rec.Chunks, Chunk{DelayMs: now.Sub(t.last).Milliseconds(), Data: string(p[:n])})
		t.last = now
	}
	if err != nil && !errors.Is(err, io.EOF) {
		t.rec.Truncated = true
	}
	return n, err
}

func (t *teeBody) Close() error { return t.rc.Close() }

// Save writes a recording as indented JSON.
func Save(path string, rec *Recording) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Load reads a recording file.
func Load(path string) (*Recording, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rec Recording
	if err := json.NewDecoder(bytes.NewReader(b)).Decode(&rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// Handler plays a recording back. speed > 1 compresses delays; 0 disables them.
func Handler(rec *Recording, speed float64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range rec.Header {
			w.Header()[k] = v
		}
		w.Header().Del("Content-Length")
		w.WriteHeader(rec.Status)
		flusher, _ := w.(http.Flusher)
		for _, c := range rec.Chunks {
			if speed > 0 && c.DelayMs > 0 {
				select {
				case <-time.After(time.Duration(float64(c.DelayMs) * float64(time.Millisecond) / speed)):
				case <-r.Context().Done():
					return
				}
			}
			if _, err := io.WriteString(w, c.Data); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rec.Truncated {
			panic(http.ErrAbortHandler) // reproduce the upstream drop
		}
	})
}
