package replay

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/bench/mockupstream"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

type streamResult struct {
	status int
	events []string
	err    error // terminal error from the SSE reader
}

func stream(t *testing.T, client *http.Client, url string) streamResult {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"stream":true}`))
	req.Header.Set("Authorization", "Bearer sk-secret")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	res := streamResult{status: resp.StatusCode}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return res
	}
	r := sse.NewReader(resp.Body, 0)
	for {
		ev, err := r.Next()
		if err != nil {
			res.err = err
			return res
		}
		res.events = append(res.events, string(ev.Data))
	}
}

// Every mock scenario must survive record -> save -> load -> replay unchanged,
// including status codes, truncation and the absence of events.
func TestRecordAndReplayEveryScenario(t *testing.T) {
	upstream := httptest.NewServer(mockupstream.Handler())
	defer upstream.Close()

	scenarios := []string{
		mockupstream.Complete, mockupstream.CompleteNoUsage, mockupstream.ErrorBeforeFirst,
		mockupstream.ErrorInStream, mockupstream.ErrorAfterOutput, mockupstream.EmptyStream,
		mockupstream.Truncated,
	}
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			recorder := &Recorder{}
			url := upstream.URL + "/v1/chat/completions?chunks=4&interval_ms=0&ttfb_ms=0&scenario=" + scenario
			original := stream(t, &http.Client{Transport: recorder}, url)

			recs := recorder.Recordings()
			if len(recs) != 1 {
				t.Fatalf("recorded %d responses", len(recs))
			}
			path := filepath.Join(t.TempDir(), scenario+".json")
			if err := Save(path, recs[0]); err != nil {
				t.Fatal(err)
			}
			loaded, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}

			player := httptest.NewServer(Handler(loaded, 0))
			defer player.Close()
			replayed := stream(t, http.DefaultClient, player.URL)

			if replayed.status != original.status || strings.Join(replayed.events, "|") != strings.Join(original.events, "|") {
				t.Fatalf("replay differs:\noriginal %d %q\nreplayed %d %q", original.status, original.events, replayed.status, replayed.events)
			}
			if original.status != http.StatusOK {
				return
			}
			want := "eof"
			if scenario == mockupstream.Truncated {
				want = "dropped"
			}
			if got := ending(original.err); got != want {
				t.Fatalf("original ending = %s (%v); want %s", got, original.err, want)
			}
			if got := ending(replayed.err); got != want {
				t.Fatalf("replayed ending = %s (%v); want %s", got, replayed.err, want)
			}
		})
	}
}

func TestRecordingRedactsSecrets(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "session=abc")
		w.Header().Set("X-Request-Id", "req-1")
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	recorder := &Recorder{}
	resp, err := (&http.Client{Transport: recorder}).Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "r.json")
	if err := Save(path, recorder.Recordings()[0]); err != nil {
		t.Fatal(err)
	}
	raw, _ := Load(path)
	if raw.Header.Get("Set-Cookie") != "" || raw.Header.Get("X-Request-Id") != "req-1" {
		t.Fatalf("headers not redacted correctly: %v", raw.Header)
	}
	var body bytes.Buffer
	for _, c := range raw.Chunks {
		body.WriteString(c.Data)
	}
	if body.String() != "ok" {
		t.Fatalf("body = %q", body.String())
	}
}

// ending classifies how a stream finished: a clean io.EOF, or a dropped
// connection (transport error or a half-written event).
func ending(err error) string {
	if errors.Is(err, io.EOF) {
		return "eof"
	}
	return "dropped"
}
