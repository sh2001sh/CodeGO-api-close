package legacy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// V2 enc:v1 strings use SHA256(CryptoSecret), AES-GCM and raw URL base64,
// with the nonce prepended to ciphertext. V3 receives independently sealed bytes.
func (m *Importer) sourceSecret(value string) (string, error) {
	if !strings.HasPrefix(value, "enc:v1:") {
		return value, nil
	}
	if m.sourceCryptoSecret == "" {
		return "", fmt.Errorf("encrypted source value requires LEGACY_CRYPTO_SECRET")
	}
	plain, err := cmSecret(value, m.sourceCryptoSecret)
	if err != nil {
		return "", fmt.Errorf("encrypted source value cannot be decrypted with LEGACY_CRYPTO_SECRET")
	}
	return plain, nil
}

func (m *Importer) sourceOption(value string) ([]byte, error) {
	var unquoted string
	if json.Unmarshal([]byte(value), &unquoted) == nil && strings.HasPrefix(unquoted, "enc:v1:") {
		value = unquoted
	}
	wasEncrypted := strings.HasPrefix(value, "enc:v1:")
	value, err := m.sourceSecret(value)
	if err != nil {
		return nil, err
	}
	encoded := []byte(value)
	if wasEncrypted || !json.Valid(encoded) {
		encoded, err = json.Marshal(value)
	}
	return encoded, err
}
