package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testConfig() benchConfig {
	return benchConfig{URL: "http://127.0.0.1:18181", Protocol: "chat", Model: "gpt-4.1", Route: "sb-openai-direct", Scenario: "text", Concurrency: 1, Requests: 1, Users: 1, InputTokens: 64, TTFT: 3 * time.Millisecond, Tail: 6 * time.Millisecond, Interval: time.Millisecond, Timeout: time.Second}
}

func TestMockAllProtocolsAndSemanticScenarios(t *testing.T) {
	for _, protocol := range []string{"chat", "responses", "claude", "gemini", "ws"} {
		for _, scenario := range []string{"text", "thinking", "tool"} {
			t.Run(protocol+"/"+scenario, func(t *testing.T) {
				c := testConfig()
				c.Protocol = protocol
				c.Scenario = scenario
				mock := newMock(c)
				server := httptest.NewServer(mock)
				defer server.Close()
				c.URL = server.URL
				w, err := makeWorkload(c)
				if err != nil {
					t.Fatal(err)
				}
				client := localHTTPClient(time.Second, 1)
				defer client.CloseIdleConnections()
				o := requestOnce(client, c, w, 1)
				if !o.OK {
					t.Fatalf("strict stream failed: %+v", o)
				}
				if o.FirstRawMS < 0 || o.FirstTextMS < o.FirstRawMS || o.CompleteMS < o.FirstTextMS || o.RawFrames <= o.Frames {
					t.Fatalf("invalid monotonic stage timing: %+v", o)
				}
				if o.Frames != w.Frames || o.MockResponseID == "" {
					t.Fatalf("incomplete observation: %+v", o)
				}
				mock.mu.Lock()
				defer mock.mu.Unlock()
				r := mock.records[o.MockResponseID]
				if r == nil || r.BodyBytes != len(w.Body) || r.InputTokens != 64 || r.FirstTextFlushMS < r.BodyReadMS || r.TerminalMS < r.FirstTextFlushMS {
					t.Fatalf("invalid mock stage decomposition: %+v", r)
				}
			})
		}
	}
}

func TestTruncatedAndFailedTerminalNeverPassHTTP200(t *testing.T) {
	for _, protocol := range []string{"chat", "responses", "claude", "gemini", "ws"} {
		for _, mode := range []string{"truncate", "failed"} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				c := testConfig()
				c.Protocol = protocol
				if mode == "truncate" {
					c.TruncateEvery = 1
				} else {
					c.ErrorEvery = 1
				}
				server := httptest.NewServer(newMock(c))
				defer server.Close()
				c.URL = server.URL
				w, err := makeWorkload(c)
				if err != nil {
					t.Fatal(err)
				}
				client := localHTTPClient(time.Second, 1)
				defer client.CloseIdleConnections()
				o := requestOnce(client, c, w, 1)
				if o.OK || o.Error == "" {
					t.Fatalf("invalid success: %+v", o)
				}
			})
		}
	}
}

func TestRealPromptTokenCountAndLongOutput(t *testing.T) {
	for _, tokens := range []int{100000, 160000} {
		c := testConfig()
		c.InputTokens = tokens
		c.ChunkBytes = 65536
		w, err := makeWorkload(c)
		if err != nil {
			t.Fatal(err)
		}
		if w.InputTokens != tokens || w.PromptBytes < tokens || len(w.Body) < w.PromptBytes || len(w.Text) < 400000 {
			t.Fatalf("real payload not constructed: %+v", w)
		}
	}
}

func TestSSELargeFramesMultilineAndInvalidJSON(t *testing.T) {
	value := strings.Repeat("x", 100000)
	raw, _ := json.Marshal(map[string]any{"value": value})
	frames := 0
	if err := readSSE(strings.NewReader("event: delta\ndata: "+string(raw)+"\n\n"), func(b []byte) error {
		frames++
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		if m["value"] != value {
			return fmt.Errorf("large frame corrupted")
		}
		return nil
	}); err != nil || frames != 1 {
		t.Fatalf("large SSE: %d %v", frames, err)
	}
	o := observation{}
	state := streamState{o: &o, start: time.Now()}
	if err := state.frame([]byte("{bad"), "chat"); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if err := readSSE(strings.NewReader("data: {\ndata: \"value\":1}\n\n"), func(b []byte) error {
		if !json.Valid(b) {
			return fmt.Errorf("multiline data failed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLoopbackAndConfigurationBoundaries(t *testing.T) {
	for _, raw := range []string{"https://127.0.0.1:18180", "http://example.com", "http://localhost:18180", "http://127.0.0.1.evil.example", "http://user:pass@127.0.0.1", "http://127.0.0.1/path", "http://127.0.0.1?target=external"} {
		if loopbackURL(raw) == nil {
			t.Fatalf("unsafe target accepted: %s", raw)
		}
	}
	for _, raw := range []string{"http://127.0.0.1:18180", "http://[::1]:18181"} {
		if err := loopbackURL(raw); err != nil {
			t.Fatal(err)
		}
	}
	c := testConfig()
	c.Requests = 0
	if c.validate() == nil {
		t.Fatal("zero total requests accepted")
	}
	c = testConfig()
	c.Concurrency = 0
	if c.validate() == nil {
		t.Fatal("zero workers accepted")
	}
}

func TestResponsesMultipleItemsValidateDoneAgainstTheirOwnDeltas(t *testing.T) {
	for _, protocol := range []string{"responses", "ws"} {
		for _, corruptPart := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/corrupt=%v", protocol, corruptPart), func(t *testing.T) {
				o := observation{}
				state := streamState{o: &o, start: time.Now()}
				secondDone := "second"
				if corruptPart {
					secondDone = "firstsecond"
				}
				frames := []map[string]any{
					{"type": "response.output_text.delta", "item_id": "message-a", "output_index": 0, "content_index": 0, "delta": "first"},
					{"type": "response.output_text.done", "item_id": "message-a", "output_index": 0, "content_index": 0, "text": "first"},
					{"type": "response.output_text.delta", "item_id": "message-b", "output_index": 2, "content_index": 0, "delta": "second"},
					{"type": "response.output_text.done", "item_id": "message-b", "output_index": 2, "content_index": 0, "text": secondDone},
					{"type": "response.completed", "response": map[string]any{"id": "response-multiple", "status": "completed", "usage": map[string]any{"input_tokens": 64, "output_tokens": 2}}},
				}
				for _, frame := range frames {
					raw, err := json.Marshal(frame)
					if err != nil {
						t.Fatal(err)
					}
					if err = state.frame(raw, protocol); err != nil {
						t.Fatal(err)
					}
				}
				err := state.finish(workload{Text: "firstsecond", InputTokens: 64, OutputTokens: 2})
				if corruptPart && err == nil {
					t.Fatal("corrupt item done accepted despite valid aggregate deltas")
				}
				if !corruptPart && err != nil {
					t.Fatalf("valid multi-item text rejected: %v", err)
				}
			})
		}
	}
}

func TestResponsesKeepResponseIDBeforeFailureAndAcrossIDlessFrames(t *testing.T) {
	for _, protocol := range []string{"responses", "ws"} {
		for _, idAtCreation := range []bool{false, true} {
			o := observation{}
			state := streamState{o: &o, start: time.Now()}
			created := map[string]any{"type": "response.created", "response_id": "response-correlation"}
			if idAtCreation {
				created = map[string]any{"type": "response.created", "response": map[string]any{"id": "response-correlation"}}
			}
			raw, _ := json.Marshal(created)
			if err := state.frame(raw, protocol); err != nil {
				t.Fatal(err)
			}
			if err := state.frame([]byte(`{"type":"response.output_text.delta","delta":"partial"}`), protocol); err != nil {
				t.Fatal(err)
			}
			if err := state.frame([]byte(`{"type":"response.failed","error":{"message":"synthetic"}}`), protocol); err == nil {
				t.Fatal("failed response accepted")
			}
			if o.MockResponseID != "response-correlation" {
				t.Fatalf("lost correlation ID before failure: %+v", o)
			}
		}
	}
}
