package gateway

import (
	"net/http"
	"sync"
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

// clientStream owns the client connection for one request. It is shared by
// the attempt loop and the heartbeat timer, so every write takes mu.
//
// Delivery rule (plan §4 first-byte gate): headers and keep-alive comments
// stay buffered until the first data event, so failover remains invisible
// and a failure before output can still return its HTTP status.
type clientStream struct {
	w        http.ResponseWriter
	stream   bool // SSE for streaming requests, one JSON body otherwise
	protocol Protocol

	mu          sync.Mutex
	sw          *sse.Writer
	headersSent bool
	delivered   bool
	gone        bool
	heartbeat   *time.Timer
	interval    time.Duration
}

func newClientStream(w http.ResponseWriter, stream bool, requestID string, protocol Protocol) *clientStream {
	w.Header().Set("X-Request-Id", requestID)
	return &clientStream{w: w, stream: stream, protocol: protocol}
}

// startHeartbeat sends an SSE comment whenever the stream has been idle for
// interval, so proxies such as Cloudflare (100 s idle limit) keep it open
// while the upstream thinks. Non-streaming requests get no heartbeat.
func (c *clientStream) startHeartbeat(interval time.Duration) {
	if !c.stream || interval <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.interval = interval
	c.heartbeat = time.AfterFunc(interval, c.beat)
}

func (c *clientStream) beat() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gone || c.heartbeat == nil {
		return
	}
	if !c.headersSent {
		c.heartbeat.Reset(c.interval)
		return
	}
	if err := c.sw.WriteComment("ping"); err != nil {
		c.gone = true
		return
	}
	c.heartbeat.Reset(c.interval)
}

func (c *clientStream) stopHeartbeat() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.heartbeat != nil {
		c.heartbeat.Stop()
		c.heartbeat = nil
	}
}

func (c *clientStream) commitLocked() {
	if c.headersSent {
		return
	}
	c.sw = sse.NewWriter(c.w) // sets SSE headers
	c.w.WriteHeader(http.StatusOK)
	c.headersSent = true
}

// writeData forwards one upstream payload. It reports false once the client
// is gone; the caller keeps draining the upstream for usage.
func (c *clientStream) writeData(name string, payload []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gone {
		return false
	}
	if !c.stream {
		c.w.Header().Set("Content-Type", "application/json")
		c.w.WriteHeader(http.StatusOK)
		c.headersSent = true
		if _, err := c.w.Write(payload); err != nil {
			c.gone = true
			return false
		}
		c.delivered = true
		return true
	}
	c.commitLocked()
	if err := c.sw.WriteEvent(name, payload); err != nil {
		c.gone = true
		return false
	}
	c.delivered = true
	if c.heartbeat != nil {
		c.heartbeat.Reset(c.interval) // data counts as activity
	}
	return true
}

// markGone records a client disconnect detected from the request context.
func (c *clientStream) markGone() {
	c.mu.Lock()
	c.gone = true
	c.mu.Unlock()
}

func (c *clientStream) state() (delivered, gone bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.delivered, c.gone
}

// Only a protocol-defined state header is forwarded; hop-by-hop, auth and
// account headers remain private to the upstream connection.
func (c *clientStream) setCodexState(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.headersSent || c.protocol != ProtocolResponses {
		return
	}
	c.w.Header().Del("X-Codex-Turn-State")
	if value != "" {
		c.w.Header().Set("X-Codex-Turn-State", value)
	}
}

// fail reports a terminal error to the client in whatever form is still
// possible: an HTTP error before headers, an in-band SSE error event after.
func (c *clientStream) fail(e *UpstreamError) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gone {
		return
	}
	if !c.headersSent {
		if len(e.Body) > 0 { // request-scoped upstream error: pass it through
			c.w.Header().Set("Content-Type", "application/json")
			c.w.WriteHeader(e.Status)
			_, _ = c.w.Write(e.Body)
			return
		}
		writeProtocolError(c.w, c.protocol, e.Status, e.Type, e.Code, e.Message)
		return
	}
	if c.stream {
		name, body := protocolErrorJSON(c.protocol, e.Status, e.Type, e.Code, e.Message)
		_ = c.sw.WriteEvent(name, body)
	}
}

// done writes the protocol's end-of-stream marker after a clean finish.
func (c *clientStream) done() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gone || !c.stream || !c.headersSent || c.protocol != ProtocolOpenAIChat {
		return
	}
	_ = c.sw.WriteEvent("", []byte("[DONE]"))
}
