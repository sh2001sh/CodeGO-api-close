package gateway

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientAddressTrustBoundary(t *testing.T) {
	g := &Gateway{cfg: Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}}
	for _, tc := range []struct{ peer, forwarded, real, want string }{
		{"192.0.2.1:80", "203.0.113.1", "", "192.0.2.1"},
		{"10.0.0.1:80", "203.0.113.1, 10.1.0.1", "", "203.0.113.1"},
		{"10.0.0.1:80", "198.51.100.99, 203.0.113.1", "", "203.0.113.1"},
		{"10.0.0.1:80", "invalid", "", "10.0.0.1"},
		{"10.0.0.1:80", "", "203.0.113.2", "203.0.113.2"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.peer
		r.Header.Set("X-Forwarded-For", tc.forwarded)
		r.Header.Set("X-Real-IP", tc.real)
		if got := g.clientAddress(r); got != tc.want {
			t.Fatalf("peer %s forwarded %s: got %s want %s", tc.peer, tc.forwarded, got, tc.want)
		}
	}
}
