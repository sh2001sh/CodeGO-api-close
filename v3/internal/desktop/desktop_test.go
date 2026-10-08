package desktop

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSixToolConfigsKeepSecretsInEncodedPayload(t *testing.T) {
	s := New(nil, nil, Config{PublicURL: "https://codego.example"})
	for _, tool := range tools {
		p, err := s.buildConfig("sk-fixture-secret", ImportInput{Tool: tool, Model: "model\"\ninjected"})
		if err != nil {
			t.Fatal(err)
		}
		b, err := base64.StdEncoding.DecodeString(p.Config)
		if err != nil {
			t.Fatal(err)
		}
		var body any
		if json.Unmarshal(b, &body) != nil {
			t.Fatalf("%s configuration is not JSON", tool)
		}
		if !strings.Contains(string(b), "sk-fixture-secret") || p.Endpoint == "" {
			t.Fatalf("tool %s lost key or endpoint", tool)
		}
	}
	for _, tool := range []string{"claude-code", "gemini-cli", "open-code", "open-claw", "hermes-agent"} {
		if normalizeTool(tool) == "" {
			t.Fatal("alias missing", tool)
		}
	}
	if _, err := s.buildConfig("key", ImportInput{Tool: "unknown"}); err != ErrInvalid {
		t.Fatal("unknown tool accepted", err)
	}
}
func TestReportsRequireConsentAndRedactCredentials(t *testing.T) {
	raw := `{"api_key":"private","info":"Bearer secret /Users/name/folder desktop_01234567890123"}`
	in := ReportInput{Consent: true, EventName: "auth_connected", Payload: json.RawMessage(raw)}
	out, err := cleanReport(in, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private", "Bearer secret", "/Users/name", "desktop_01234567890123"} {
		if strings.Contains(string(out.Payload), secret) {
			t.Fatal("secret retained", secret)
		}
	}
	in.Consent = false
	if _, err := cleanReport(in, true); err != ErrInvalid {
		t.Fatal("missing consent accepted")
	}
	in.Consent = true
	in.Payload = json.RawMessage(`{"nested":{"key":"value"}}`)
	if _, err := cleanReport(in, true); err != ErrInvalid {
		t.Fatal("unbounded nested payload accepted")
	}
	in = ReportInput{Consent: true, ReportType: "crash", Payload: json.RawMessage(`"Authorization: Bearer secret C:\\Users\\name\\key"`)}
	out, err = cleanReport(in, false)
	if err != nil || strings.Contains(string(out.Payload), "secret") {
		t.Fatalf("diagnostics not redacted %v", err)
	}
}
func TestLimiterDoesNotTrustSpoofedForwardedHeader(t *testing.T) {
	var l limiter
	now := time.Now()
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	for i := 0; i < 60; i++ {
		r.Header.Set("X-Forwarded-For", string(rune(i)))
		if !l.allow(r, now) {
			t.Fatal("rejected before budget")
		}
	}
	if l.allow(r, now) {
		t.Fatal("spoofed forwarded values bypassed rate limit")
	}
	if !l.allow(r, now.Add(time.Minute)) {
		t.Fatal("window did not reset")
	}
}
func TestDesktopHandlerRejectsCrossSiteBeforeStorage(t *testing.T) {
	s := New(nil, nil, Config{PublicURL: "https://codego.example"})
	r := httptest.NewRequest("POST", "https://codego.example/api/desktop/auth/session", strings.NewReader(`{"device_name":"test"}`))
	r.Header.Set("Origin", "https://attacker.example")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-site=%d", w.Code)
	}
}

func TestDesktopReleasedScopesKeepPrefixWhileV3UsesNativeNames(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/desktop/devices", nil)
	in := []string{"account:read", "tokens:write"}
	out := protocolScopes(r, in)
	if out[0] != "desktop:account:read" || out[1] != "desktop:tokens:write" || in[0] != "account:read" {
		t.Fatal("released scope protocol changed", out, in)
	}
	r.Header.Set("X-CodeGo-API-Version", "3")
	if out := protocolScopes(r, in); out[0] != "account:read" {
		t.Fatal("native scopes changed", out)
	}
}
