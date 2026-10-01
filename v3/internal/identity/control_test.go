package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func testControl(t *testing.T) (*Control, *time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	c, err := NewControl(nil, ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32), PublicURL: "https://codego.test", Now: func() time.Time { return now }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c, &now
}

func TestSessionSignatureExpiryAndAlgorithm(t *testing.T) {
	c, now := testControl(t)
	token, err := c.accessToken("session-id", 42, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	claims, err := c.parseToken(token)
	if err != nil || claims.UserID != 42 || claims.SessionID != "session-id" {
		t.Fatalf("claims=%+v err=%v", claims, err)
	}
	parts := strings.Split(token, ".")
	for _, invalid := range []string{parts[0] + "." + parts[1] + ".invalid", strings.Replace(token, parts[0], "eyJhbGciOiJub25lIn0", 1), "", token + ".extra"} {
		if _, err := c.parseToken(invalid); !errors.Is(err, ErrCredentials) {
			t.Fatalf("forged token accepted: %v", err)
		}
	}
	*now = now.Add(time.Minute)
	if _, err := c.parseToken(token); !errors.Is(err, ErrCredentials) {
		t.Fatalf("expired token accepted: %v", err)
	}
}

func TestAPIKeyEncryptionRejectsTampering(t *testing.T) {
	c, _ := testControl(t)
	encrypted, err := c.encryptKey("sk-secret")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := c.decryptKey(encrypted)
	if err != nil || plain != "sk-secret" {
		t.Fatalf("plain=%q err=%v", plain, err)
	}
	if bytes.Contains(encrypted, []byte("sk-secret")) {
		t.Fatal("ciphertext contains plaintext")
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err := c.decryptKey(encrypted); err == nil {
		t.Fatal("modified ciphertext accepted")
	}
	if _, err := c.decryptKey([]byte{0}); err == nil {
		t.Fatal("truncated ciphertext accepted")
	}
}

// Regression: imported v2 keys use the shared nonce||AES-GCM format.
func TestDecryptImportedAPIKey(t *testing.T) {
	c, _ := testControl(t)
	enc, err := catalog.NewAESGCM(bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	b, err := enc.Encrypt([]byte("sk-imported"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := c.decryptKey(b)
	if err != nil || plain != "sk-imported" {
		t.Fatalf("imported key cannot be disclosed: %q %v", plain, err)
	}
}

func TestCrossSiteRegistrationBlockedBeforeDatabase(t *testing.T) {
	c, _ := testControl(t)
	r := httptest.NewRequest(http.MethodPost, "https://codego.test/api/user/register", strings.NewReader(`{"username":"alice","password":"long-password"}`))
	r.Header.Set("Origin", "https://attacker.test")
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross site status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestInvalidKeyRestrictionsRejected(t *testing.T) {
	if err := validateKey(&KeyInput{AllowedCIDRs: []string{"not-an-ip"}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid cidr accepted: %v", err)
	}
}

func TestCachedProfilePreservesExplicitEmptyRestrictions(t *testing.T) {
	for _, input := range []*KeyProfile{{}, {AllowedModels: []string{}, AllowedCIDRs: []netip.Prefix{}}} {
		blob, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		var cached KeyProfile
		if err = json.Unmarshal(blob, &cached); err != nil {
			t.Fatal(err)
		}
		if cached.AllowsModel("any") != input.AllowsModel("any") || cached.AllowsAddr(netip.MustParseAddr("127.0.0.1")) != input.AllowsAddr(netip.MustParseAddr("127.0.0.1")) {
			t.Fatalf("cache erased restriction: %s", blob)
		}
	}
}

func TestLoginAttemptsAreBoundedAndResetAfterWindow(t *testing.T) {
	c, now := testControl(t)
	for i := 0; i < 10; i++ {
		if !c.limiter.allow("127.0.0.1") {
			t.Fatalf("attempt %d unexpectedly rejected", i)
		}
	}
	if c.limiter.allow("127.0.0.1") {
		t.Fatal("eleventh attempt accepted")
	}
	*now = now.Add(time.Minute)
	if !c.limiter.allow("127.0.0.1") {
		t.Fatal("new window still blocked")
	}
}

func TestControlRejectsWeakKeyConfiguration(t *testing.T) {
	if _, err := NewControl(nil, ControlConfig{}, nil); err == nil {
		t.Fatal("accepted missing encryption and session secrets")
	}
}

func TestUnknownRestrictionsAreRejectedRatherThanSilentlyIgnored(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/token/", strings.NewReader(`{"name":"restricted","unimplemented_restriction":true}`))
	w := httptest.NewRecorder()
	var input KeyRecord
	if err := decodeControl(w, r, &input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsupported restriction silently accepted: %v", err)
	}
	r = httptest.NewRequest(http.MethodPut, "/api/token/", strings.NewReader(`{"id":1,"name":"key","status":"disabled","key_prefix":"sk-abcd","created_at":"2026-09-30T00:00:00Z","last_used_at":null}`))
	if err := decodeControl(w, r, &input); err != nil {
		t.Fatalf("existing key DTO cannot be updated: %v", err)
	}
}
