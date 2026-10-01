package channelmarket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestMultiplierExactBoundary(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int64
	}{{"0.1", 100000}, {"0.0000015", 2}, {"999.999999", 999999999}} {
		got, err := multiplier(json.Number(tc.raw))
		if err != nil || got != tc.want {
			t.Fatalf("%s -> %d %v", tc.raw, got, err)
		}
	}
	for _, raw := range []string{"-1", "0", "NaN", "1e30", "0.00000001"} {
		if _, err := multiplier(json.Number(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
func TestPrivateAndReservedProbeAddressesRejected(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "198.18.0.1", "192.0.2.1", "2001:db8::1", "0.1.2.3", "64:ff9b::7f00:1", "2002:7f00:1::1", "2001::1"} {
		if publicAddress(netip.MustParseAddr(ip)) {
			t.Fatalf("SSRF allowed %s", ip)
		}
	}
	if !publicAddress(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("public address rejected")
	}
}
func TestHTTPDeniesMissingIdentityAndCrossSiteMutation(t *testing.T) {
	s := New(nil, nil, nil, Config{}, nil)
	mux := http.NewServeMux()
	s.Register(mux, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/marketplace/channels", strings.NewReader(`{}`)))
	if w.Code != 403 {
		t.Fatalf("anonymous mutation: %d", w.Code)
	}
	mux = http.NewServeMux()
	s.Register(mux, func(*http.Request) (Actor, error) { return Actor{UserID: 1}, nil })
	r := httptest.NewRequest(http.MethodPost, "http://control.test/api/marketplace/channels", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://attacker.test")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-site mutation: %d", w.Code)
	}
}
