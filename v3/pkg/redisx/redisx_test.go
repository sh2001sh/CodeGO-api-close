package redisx

import (
	"net"
	"strings"
	"testing"
	"time"
)

// Connect pings before returning, so an unreachable Redis is reported at
// start instead of on the first request.
func TestConnectFailsFastWhenUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // nothing listens here now

	start := time.Now()
	c, err := Connect(Config{Addr: addr})
	if err == nil {
		_ = c.Close()
		t.Fatal("Connect to a closed port succeeded")
	}
	if !strings.Contains(err.Error(), "ping") {
		t.Errorf("error = %v; want the ping failure", err)
	}
	if d := time.Since(start); d > 6*time.Second {
		t.Errorf("Connect took %v; want it bounded by the 5s ping timeout", d)
	}
}
