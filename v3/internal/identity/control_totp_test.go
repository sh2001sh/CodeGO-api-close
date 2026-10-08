package identity

import (
	"strings"
	"testing"
	"time"
)

func TestTOTPProfileRFCVectorExpiryAndReplay(t *testing.T) {
	// RFC 6238 SHA-1 vector at 59 seconds truncated to six digits.
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	code, err := totpCode(secret, 1)
	if err != nil || code != "287082" {
		t.Fatalf("RFC vector=%s err=%v", code, err)
	}
	if counter, ok := totpCounter(secret, code, time.Unix(59, 0), -1); !ok || counter != 1 {
		t.Fatalf("valid code counter=%d ok=%v", counter, ok)
	}
	if _, ok := totpCounter(secret, code, time.Unix(59, 0), 1); ok {
		t.Fatal("used TOTP counter accepted")
	}
	if _, ok := totpCounter(secret, code, time.Unix(120, 0), -1); ok {
		t.Fatal("expired TOTP accepted")
	}
	for _, bad := range []string{"", "28708", "+87082", "abcdef", "2870827"} {
		if _, ok := totpCounter(secret, bad, time.Unix(59, 0), -1); ok {
			t.Fatalf("bad code %q accepted", bad)
		}
	}
	if _, err := totpCode("invalid", 1); err == nil {
		t.Fatal("invalid base32 secret accepted")
	}
}

func TestBackupFormatMatchesImportedHashesAndURIEncoding(t *testing.T) {
	if got := normalizeBackupCode("abcd1234"); got != "ABCD-1234" {
		t.Fatalf("normalized=%q", got)
	}
	for _, bad := range []string{"", "abcd-123", "ABCD-12_4", "ABCD--12 4"} {
		if normalizeBackupCode(bad) != "" {
			t.Fatalf("invalid backup %q accepted", bad)
		}
	}
	codes, err := newBackupCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 4 {
		t.Fatalf("codes=%d", len(codes))
	}
	for _, code := range codes {
		if len(code) != 9 || normalizeBackupCode(code) != code {
			t.Fatalf("bad backup code %q", code)
		}
	}
	uri := twoFactorURI("ABC234", "alice@example.test")
	if !strings.Contains(uri, "issuer=CodeGo") || !strings.Contains(uri, "period=30") {
		t.Fatalf("authenticator URI=%s", uri)
	}
}

func TestEmailInputAndProofPurposeBoundaries(t *testing.T) {
	for _, bad := range []string{"", "Name <a@example.test>", "a@example.test\r\nBCC: b@test", strings.Repeat("a", 255) + "@test"} {
		if _, err := normalizedEmail(bad); err == nil {
			t.Fatalf("bad email accepted %q", bad)
		}
	}
	if email, err := normalizedEmail(" Alice@Example.Test "); err != nil || email != "alice@example.test" {
		t.Fatalf("email=%q err=%v", email, err)
	}
	c, _ := testControl(t)
	one := string(c.emailProofHash("alice@test", "registration", "123456"))
	if one == string(c.emailProofHash("alice@test", "binding", "123456")) || one == string(c.emailProofHash("bob@test", "registration", "123456")) {
		t.Fatal("email proofs cross purpose/owner boundary")
	}
}
