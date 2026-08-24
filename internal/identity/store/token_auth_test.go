package store

import (
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/constant"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
)

func TestTokenInvalidReasonClassifiesCachedState(t *testing.T) {
	now := platformruntime.GetTimestamp()
	tests := []struct {
		name   string
		token  identityschema.Token
		reason string
	}{
		{name: "disabled", token: identityschema.Token{Status: constant.TokenStatusDisabled, UnlimitedQuota: true}, reason: "disabled"},
		{name: "expired status", token: identityschema.Token{Status: constant.TokenStatusExpired, UnlimitedQuota: true}, reason: "expired"},
		{name: "expired time", token: identityschema.Token{Status: constant.TokenStatusEnabled, ExpiredTime: now - 1, UnlimitedQuota: true}, reason: "expired"},
		{name: "exhausted status", token: identityschema.Token{Status: constant.TokenStatusExhausted, UnlimitedQuota: true}, reason: "exhausted"},
		{name: "exhausted quota", token: identityschema.Token{Status: constant.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 0}, reason: "exhausted"},
		{name: "valid", token: identityschema.Token{Status: constant.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1}, reason: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.reason, tokenInvalidReason(&test.token))
		})
	}
}

func TestTokenKeyFingerprintDoesNotExposeKey(t *testing.T) {
	key := "sk-sensitive-token-value"
	fingerprint := tokenKeyFingerprint(key)
	require.NotEmpty(t, fingerprint)
	require.Len(t, fingerprint, 12)
	require.False(t, strings.Contains(fingerprint, key))
	require.Equal(t, fingerprint, tokenKeyFingerprint(key))
}
