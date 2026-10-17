package files

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeliverySignatureBindsFileExpiryAndSecret(t *testing.T) {
	t.Setenv("FILE_DELIVERY_BASE_URL", "https://example.com")
	t.Setenv("FILE_DELIVERY_SIGNING_SECRET", "test-signing-secret")
	const id = "file-codego-test"
	now := time.Now().UTC()
	raw, err := BuildSignedDeliveryURL(id, now)
	require.NoError(t, err)
	u, err := url.Parse(raw)
	require.NoError(t, err)
	expires, signature := u.Query().Get("expires"), u.Query().Get("signature")
	require.NoError(t, VerifyDeliveryToken(id, expires, signature, now))
	for _, candidate := range []struct{ id, expires, sig string }{
		{"file-codego-other", expires, signature},
		{id, "999999999999", signature},
		{id, expires, strings.Repeat("0", 64)},
		{id, expires, "invalid-hex"},
		{id, expires, ""},
	} {
		require.ErrorIs(t, VerifyDeliveryToken(candidate.id, candidate.expires, candidate.sig, now), ErrInvalidDeliveryToken)
	}
	require.ErrorIs(t, VerifyDeliveryToken(id, expires, signature, now.Add(time.Hour)), ErrInvalidDeliveryToken)
	t.Setenv("FILE_DELIVERY_SIGNING_SECRET", "rotated-secret")
	require.ErrorIs(t, VerifyDeliveryToken(id, expires, signature, now), ErrInvalidDeliveryToken)
	t.Setenv("FILE_DELIVERY_SIGNING_SECRET", "")
	require.ErrorIs(t, VerifyDeliveryToken(id, expires, signature, now), ErrInvalidDeliveryToken)
}
