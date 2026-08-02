package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeGitHubResourceURL(t *testing.T) {
	normalized, repository, err := NormalizeGitHubResourceURL(
		"https://github.com/sh2001sh/new-api/tree/main/scripts",
	)
	require.NoError(t, err)
	require.Equal(t, "https://github.com/sh2001sh/new-api/tree/main/scripts", normalized)
	require.Equal(t, "https://github.com/sh2001sh/new-api", repository)

	normalized, repository, err = NormalizeGitHubResourceURL(
		"https://github.com/sh2001sh/new-api.git",
	)
	require.NoError(t, err)
	require.Equal(t, "https://github.com/sh2001sh/new-api", normalized)
	require.Equal(t, "https://github.com/sh2001sh/new-api", repository)
}

func TestNormalizeGitHubResourceURLRejectsLookalikeHosts(t *testing.T) {
	_, _, err := NormalizeGitHubResourceURL("https://github.com.evil.example/owner/repo")
	require.Error(t, err)

	_, _, err = NormalizeGitHubResourceURL("https://user@github.com/owner/repo")
	require.Error(t, err)
}
