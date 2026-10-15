package legacy

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestEncryptedChannelRoutingRestoresAndRejectsWrongSecret(t *testing.T) {
	key := sha256.Sum256([]byte("channel-routing-fixture"))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	seal := func(s string) string {
		nonce := make([]byte, gcm.NonceSize())
		return "enc:v1:" + base64.RawURLEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(s), nil))
	}
	raw, err := json.Marshal(map[string]any{"id": 1, "type": 1, "name": "fixture", "base_url": seal("https://upstream.example"), "key": seal("fixture-key"), "status": 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("V3_MIGRATION_SOURCE_CRYPTO_SECRET", "channel-routing-fixture")
	c, provider, err := decodeChannel(raw)
	if err != nil || provider != "openai" || c.BaseURL != "https://upstream.example" || c.Key != "fixture-key" {
		t.Fatalf("routing restoration failed: %v", err)
	}
	for _, secret := range []string{"wrong-secret", ""} {
		t.Setenv("V3_MIGRATION_SOURCE_CRYPTO_SECRET", secret)
		if _, _, err := decodeChannel(raw); err == nil {
			t.Fatal("encrypted routing accepted without correct source secret")
		}
	}
}
