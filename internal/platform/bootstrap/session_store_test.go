package bootstrap

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewCookieSessionStoreRequiresStrongSecret(t *testing.T) {
	t.Parallel()

	_, err := newCookieSessionStore("too-short")
	require.ErrorContains(t, err, "at least 32")
}

func TestNewCookieSessionStoreAcceptsLocalDevelopmentSecret(t *testing.T) {
	t.Parallel()

	store, err := newCookieSessionStore("local-development-session-secret-123456")
	require.NoError(t, err)
	require.NotNil(t, store)
}
