package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func completedMockRecord(t *testing.T, mock *mockServer) mockRecord {
	t.Helper()
	// Completion includes a bounded control write and then a bounded drain;
	// it is separate from the prompt-cancellation deadline asserted below.
	deadline := time.NewTimer(mockWSControlTimeout + mockWSCloseDrainTimeout + time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		mock.mu.Lock()
		for _, record := range mock.records {
			// Zero is a valid elapsed duration on Windows. The handler writes
			// its final record before decrementing active, so activity is the
			// completion signal rather than a nonzero clock sample.
			if mock.active.Load() == 0 {
				result := *record
				mock.mu.Unlock()
				return result
			}
		}
		mock.mu.Unlock()
		select {
		case <-deadline.C:
			mock.mu.Lock()
			records, err := json.Marshal(mock.records)
			mock.mu.Unlock()
			t.Fatalf("mock did not finish its reader and handler within its shutdown budget: active=%d records=%s encode_error=%v", mock.active.Load(), records, err)
		case <-ticker.C:
		}
	}
}

func TestMockWebSocketPingLargeTailAndCloseHandshake(t *testing.T) {
	c := testConfig()
	c.Protocol = "ws"
	c.InputTokens = 160000
	c.Interval = time.Millisecond
	c.Tail = 274 * time.Millisecond
	c.ChunkBytes = 256
	mock := newMock(c)
	server := httptest.NewServer(mock)
	defer server.Close()
	w, err := makeWorkload(c)
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+w.Path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	pongs := 0
	conn.SetPongHandler(func(payload string) error {
		if payload != "probe" {
			t.Errorf("unexpected pong payload %q", payload)
		}
		pongs++
		return nil
	})
	if err := conn.WriteMessage(websocket.TextMessage, w.Body); err != nil {
		t.Fatal(err)
	}
	o := observation{}
	state := streamState{o: &o, start: time.Now()}
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				t.Fatalf("unclean close after %d text frames: %v", o.Frames, err)
			}
			break
		}
		if err := state.frame(data, "ws"); err != nil {
			t.Fatal(err)
		}
		var frame map[string]any
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatal(err)
		}
		if str(frame["type"]) == "response.output_text.delta" && (o.Frames == 1 || o.Frames == 200) {
			if err := conn.WriteControl(websocket.PingMessage, []byte("probe"), time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := state.finish(w); err != nil {
		t.Fatal(err)
	}
	if o.Frames != 275 || o.TextBytes <= 65536 || pongs != 2 {
		t.Fatalf("incomplete large-tail/control validation: frames=%d bytes=%d pongs=%d", o.Frames, o.TextBytes, pongs)
	}
	record := completedMockRecord(t, mock)
	if record.WSPingsReceived != 2 || record.WSPongsSent != 2 || !record.WSCloseSent || !record.WSCloseHandshake || record.WSPeerCloseCode != websocket.CloseNormalClosure || record.WSCloseTimedOut || record.Cancelled || record.WSReadError != "" || record.WSWriteError != "" || record.WSControlError != "" {
		t.Fatalf("invalid control lifecycle: %+v", record)
	}
	t.Logf("frames=%d bytes=%d input_tokens=%d ping/pong=%d/%d close=%d handshake=%v active=%d", o.Frames, o.TextBytes, o.InputTokens, record.WSPingsReceived, record.WSPongsSent, record.WSPeerCloseCode, record.WSCloseHandshake, mock.active.Load())
}

func TestMockWebSocketPeerDisconnectCancelsInitialWait(t *testing.T) {
	for _, graceful := range []bool{false, true} {
		t.Run(map[bool]string{false: "abrupt", true: "close-handshake"}[graceful], func(t *testing.T) {
			c := testConfig()
			c.Protocol = "ws"
			c.TTFT = 10 * time.Second
			mock := newMock(c)
			server := httptest.NewServer(mock)
			defer server.Close()
			w, err := makeWorkload(c)
			if err != nil {
				t.Fatal(err)
			}
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+w.Path, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			if err := conn.WriteMessage(websocket.TextMessage, w.Body); err != nil {
				t.Fatal(err)
			}
			// Ensure the request was accepted before testing interruption.
			if _, _, err := conn.ReadMessage(); err != nil {
				t.Fatal(err)
			}
			if graceful {
				if err := conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
							t.Fatal(err)
						}
						break
					}
				}
			} else if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
			record := completedMockRecord(t, mock)
			if !record.Cancelled || record.CancelledMS-record.FirstFrameMS >= 1000 || record.TerminalMS != 0 || record.TextFrames != 0 || record.WSCloseTimedOut {
				t.Fatalf("peer interruption did not stop initial wait promptly: %+v", record)
			}
			if graceful && (!record.WSCloseHandshake || record.WSPeerCloseCode != websocket.CloseNormalClosure) {
				t.Fatalf("missing peer close handshake: %+v", record)
			}
			if !graceful && record.WSReadError == "" {
				t.Fatal("abrupt peer disconnect was not recorded")
			}
		})
	}
}

func TestMockWebSocketUnacknowledgedCloseHasBoundedDrain(t *testing.T) {
	c := testConfig()
	c.Protocol = "ws"
	mock := newMock(c)
	server := httptest.NewServer(mock)
	defer server.Close()
	w, err := makeWorkload(c)
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+w.Path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// Read the server's Close without sending an acknowledgment or closing
	// the socket, forcing the mock's bounded drain rather than a peer EOF.
	conn.SetCloseHandler(func(int, string) error { return nil })
	if err := conn.WriteMessage(websocket.TextMessage, w.Body); err != nil {
		t.Fatal(err)
	}
	o := observation{}
	state := streamState{o: &o, start: time.Now()}
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				t.Fatal(err)
			}
			break
		}
		if err := state.frame(data, "ws"); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.finish(w); err != nil {
		t.Fatal(err)
	}
	record := completedMockRecord(t, mock)
	drainMS := record.DoneMS - record.TerminalMS
	if !record.WSCloseTimedOut || !record.WSCloseSent || record.WSCloseHandshake || record.Cancelled || record.CancelledMS != 0 || drainMS < float64(mockWSCloseDrainTimeout/time.Millisecond)-100 || drainMS > float64((mockWSCloseDrainTimeout+time.Second)/time.Millisecond) {
		t.Fatalf("unacknowledged Close was not bounded independently of stream integrity: %+v", record)
	}
	t.Logf("terminal=%s drain_ms=%.1f timed_out=%v cancelled=%v active=%d", o.Terminal, drainMS, record.WSCloseTimedOut, record.Cancelled, mock.active.Load())
}
