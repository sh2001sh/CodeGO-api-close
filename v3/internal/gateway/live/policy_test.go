package live

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestPolicyEnforcesModelCIDRGroupAndTrustedProxy(t *testing.T) {
	h := &Handler{cfg: Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}}
	p := gateway.Principal{UserID: 1, KeyID: 2, Group: "member", AllowedModels: []string{"permitted"}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, AllowedGroups: []string{"member"}}
	r := httptest.NewRequest(http.MethodGet, "/v1/files", nil)
	r.RemoteAddr = "203.0.113.2:10"
	r.Header.Set("X-Forwarded-For", "192.0.2.1")
	if err := h.validatePolicy(p, "permitted", r); err == nil {
		t.Fatal("direct caller spoofed forwarded IP")
	}
	r.RemoteAddr = "10.1.2.3:10"
	r.Header.Set("X-Forwarded-For", "198.51.100.1, 192.0.2.1, 10.2.3.4")
	if err := h.validatePolicy(p, "permitted", r); err != nil {
		t.Fatalf("trusted proxy chain rejected: %v", err)
	}
	if err := h.validatePolicy(p, "forbidden", r); err == nil {
		t.Fatal("model policy bypassed")
	}
	p.Group = "foreign"
	if err := h.validatePolicy(p, "permitted", r); err == nil {
		t.Fatal("group policy bypassed")
	}
	p.Group = "member"
	p.AllowedModels = []string{}
	if err := h.validatePolicy(p, "permitted", r); err == nil {
		t.Fatal("empty model allowlist should deny all")
	}
	if err := h.validatePolicy(p, "", r); err != nil {
		t.Fatalf("model-free file request rejected: %v", err)
	}
	p.AllowedCIDRs = []netip.Prefix{}
	if err := h.validatePolicy(p, "", r); err == nil {
		t.Fatal("empty CIDR allowlist should deny all")
	}
}

func TestPolicyCallbackEnforcedOnFileAndSocketAdmission(t *testing.T) {
	h := &Handler{cfg: Config{Auth: backgroundAuth{}, ValidateRequest: func(gateway.Principal, string, *http.Request) error { return gateway.ErrInvalidKey }}}
	r := httptest.NewRequest(http.MethodGet, "/v1/files", nil)
	r.Header.Set("Authorization", "Bearer owner")
	w := httptest.NewRecorder()
	_, ok := h.authorize(w, r)
	if ok || w.Code != 403 {
		t.Fatalf("policy not enforced: ok=%v status=%d", ok, w.Code)
	}
}

type liveAuthFailures struct{ failed int }

func (*liveAuthFailures) Blocked(string) bool { return false }
func (l *liveAuthFailures) Failed(string)     { l.failed++ }

func TestValidBrowserProtocolKeyDoesNotConsumeAuthFailureBudget(t *testing.T) {
	failures := &liveAuthFailures{}
	h := &Handler{cfg: Config{Auth: backgroundAuth{}, AuthFailures: failures}}
	r := httptest.NewRequest(http.MethodGet, "/v1/realtime?model=gpt", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "realtime, openai-insecure-api-key.owner, openai-beta.realtime-v1")
	w := httptest.NewRecorder()
	if _, ok := h.authorize(w, r); !ok {
		t.Fatalf("valid browser key rejected %d", w.Code)
	}
	if failures.failed != 0 {
		t.Fatalf("valid key consumed failure budget=%d", failures.failed)
	}
	r.Header.Del("Sec-WebSocket-Protocol")
	w = httptest.NewRecorder()
	if _, ok := h.authorize(w, r); ok || failures.failed != 1 {
		t.Fatalf("missing key failures=%d", failures.failed)
	}
}
