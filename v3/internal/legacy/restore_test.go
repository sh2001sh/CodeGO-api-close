package legacy

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestSourceSecretRequiresCorrectKeyAndResealsStringSemantics(t *testing.T) {
	const sourceKey = "local-unit-test-source"
	key := sha256.Sum256([]byte(sourceKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"original-source-token", "123", "true", `{"key":"value"}`} {
		nonce := make([]byte, gcm.NonceSize())
		encoded := "enc:v1:" + base64.RawURLEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil))
		for _, badKey := range []string{"", "incorrect-key"} {
			if _, err := (&Importer{sourceCryptoSecret: badKey}).sourceSecret(encoded); err == nil {
				t.Fatal("encrypted source accepted absent/wrong key")
			}
		}
		importer := &Importer{sourceCryptoSecret: sourceKey}
		got, err := importer.sourceSecret(encoded)
		if err != nil || got != plain {
			t.Fatal("source secret decrypted differently")
		}
		for _, input := range []string{encoded, `"` + encoded + `"`} {
			data, err := importer.sourceOption(input)
			if err != nil {
				t.Fatal(err)
			}
			var value string
			if json.Unmarshal(data, &value) != nil || value != plain {
				t.Fatal("encrypted source string became a numeric/object setting")
			}
		}
	}
	for _, value := range []string{"enc:v1:???", "enc:v1:AA", "enc:v1:"} {
		if _, err := (&Importer{sourceCryptoSecret: sourceKey}).sourceSecret(value); err == nil {
			t.Fatal("malformed encrypted source accepted")
		}
	}
}

func TestRestoredDesktopScopesKeepLegacyAccessWithoutExpansion(t *testing.T) {
	got, err := projectRestoredScopes("desktop:account:read,desktop:config:write,desktop:account:read,")
	if err != nil || strings.Join(got, ",") != "account:read,config:write,telemetry:write" {
		t.Fatalf("scopes=%v err=%v", got, err)
	}
	for _, empty := range []string{"", ", ,"} {
		got, err = projectRestoredScopes(empty)
		if err != nil || len(got) != 7 {
			t.Fatal("old grants without explicit scope lost default access")
		}
	}
	if _, err = projectRestoredScopes("desktop:root:write"); err == nil {
		t.Fatal("unsupported privileged desktop scope accepted")
	}
}

func TestRestoredAuthRejectsMalformedPersistentValues(t *testing.T) {
	for _, value := range []string{
		`{"user_id":7,"secret":"secret","is_enabled":true}`,
		`{"user_id":7,"secret":null,"is_enabled":true,"failed_attempts":0}`,
		`{"user_id":7,"secret":"secret","is_enabled":true,"failed_attempts":-1}`,
	} {
		if _, err := projectRestoredFactor(json.RawMessage(value)); err == nil {
			t.Fatal("malformed persistent two-factor state accepted")
		}
	}
	for _, secret := range []string{"not-valid-base32", "JBSWY3DP", ""} {
		if validateRestoredTOTP(secret) == nil {
			t.Fatal("invalid stored TOTP seed accepted")
		}
	}
	if validateRestoredTOTP("JBSWY3DPEHPK3PXP") != nil {
		t.Fatal("valid 10-byte original TOTP seed rejected")
	}
	if _, err := projectRestoredBackup(json.RawMessage(`{"id":1,"user_id":7,"code_hash":"not-bcrypt","is_used":false}`)); err == nil {
		t.Fatal("malformed stored backup hash accepted")
	}
	if _, err := projectRestoredSession(json.RawMessage(`{"session_id":"s","user_code":"u","device_name":"PC","status":"approved","created_at":1,"expires_at":10}`)); err == nil {
		t.Fatal("approved source session without credential revived")
	}
}
