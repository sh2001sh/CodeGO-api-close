package security

import (
	"strings"
	"testing"

	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
)

func TestSecretCipherRoundTrip(t *testing.T) {
	original := platformconfig.CryptoSecret
	platformconfig.CryptoSecret = "marketplace-secret-cipher-test"
	t.Cleanup(func() { platformconfig.CryptoSecret = original })

	encrypted, err := EncryptSecret("sk-sensitive")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "sk-sensitive" || !strings.HasPrefix(encrypted, encryptedSecretPrefix) {
		t.Fatalf("expected encrypted payload, got %q", encrypted)
	}
	decrypted, err := DecryptSecret(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "sk-sensitive" {
		t.Fatalf("unexpected plaintext %q", decrypted)
	}
}
