package identity

import "testing"

func TestOAuthReturnOnlyAcceptsLocalPaths(t *testing.T) {
	for _, path := range []string{"/api/oidc/authorize?client_id=nodebb&scope=openid", "/dashboard", "/path?q=https%3A%2F%2Felsewhere.test"} {
		if got := localOAuthReturnTo(path); got != path {
			t.Fatalf("local path lost: %q -> %q", path, got)
		}
	}
	for _, path := range []string{"", "https://evil.test", "//evil.test/path", "/%2f/evil.test", "/\\evil.test", "/%5cevil.test", "/%0d%0aLocation:evil", "dashboard"} {
		if got := localOAuthReturnTo(path); got != "" {
			t.Fatalf("unsafe return admitted: %q -> %q", path, got)
		}
	}
}
