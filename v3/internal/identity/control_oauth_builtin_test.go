package identity

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func builtinSettings() map[string]string {
	return map[string]string{
		"GitHubOAuthEnabled": "true", "GitHubClientId": "github-client", "GitHubClientSecret": "github-secret",
		"LinuxDOOAuthEnabled": "true", "LinuxDOClientId": "linuxdo-client", "LinuxDOClientSecret": "linuxdo-secret", "LinuxDOMinimumTrustLevel": "2",
		"discord.enabled": "true", "discord.client_id": "12345678901234567890", "discord.client_secret": "discord-secret",
		"oidc.enabled": "true", "oidc.client_id": "oidc-client", "oidc.client_secret": "oidc-secret",
		"oidc.authorization_endpoint": "https://id.test/auth", "oidc.token_endpoint": "https://id.test/token", "oidc.user_info_endpoint": "https://id.test/userinfo",
		"ServerAddress": "https://codego.test",
	}
}

func TestBuiltinOAuthMappingsAndTrustPolicy(t *testing.T) {
	settings := builtinSettings()
	for _, tc := range []struct {
		name, id, userURL, subjectField string
		auth                            int
		scopes                          []string
	}{
		{"github", "github-client", "https://api.github.com/user", "id", 1, []string{"user:email"}},
		{"linuxdo", "linuxdo-client", "https://connect.linux.do/api/user", "id", 2, nil},
		{"discord", "12345678901234567890", "https://discord.com/api/v10/users/@me", "id", 1, []string{"identify", "openid"}},
		{"oidc", "oidc-client", "https://id.test/userinfo", "sub", 1, []string{"openid", "profile", "email"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := buildBuiltinOAuthProvider(tc.name, "", settings)
			if err != nil {
				t.Fatal(err)
			}
			if p.ClientID != tc.id || p.UserInfoURL != tc.userURL || p.UserIDField != tc.subjectField || p.AuthStyle != tc.auth || !reflect.DeepEqual(p.Scopes, tc.scopes) || p.RedirectURL != "https://codego.test/oauth/"+tc.name {
				t.Fatalf("incorrect mapping for %s", tc.name)
			}
		})
	}
	p, err := buildBuiltinOAuthProvider("linuxdo", "https://control.test", settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"trust_level":1}`, `{}`, `{"trust_level":"not-numeric"}`, `{"trust_level":null}`} {
		if err = checkOAuthPolicy([]byte(body), p.AccessPolicy); !errors.Is(err, ErrForbidden) {
			t.Fatalf("trust bypass for %s: %v", body, err)
		}
	}
	if err = checkOAuthPolicy([]byte(`{"trust_level":2}`), p.AccessPolicy); err != nil {
		t.Fatalf("trust boundary denied: %v", err)
	}
	delete(settings, "LinuxDOMinimumTrustLevel")
	p, err = buildBuiltinOAuthProvider("linuxdo", "", settings)
	if err != nil || checkOAuthPolicy([]byte(`{"trust_level":0}`), p.AccessPolicy) != nil {
		t.Fatalf("v2 default trust minimum changed: %v", err)
	}
}

func TestBuiltinOAuthMalformedConfigurationFailsClosed(t *testing.T) {
	for _, tc := range []struct{ name, provider, key, value string }{
		{"bad enabled flag", "github", "GitHubOAuthEnabled", "yes"},
		{"missing client", "github", "GitHubClientId", ""},
		{"missing secret", "github", "GitHubClientSecret", ""},
		{"negative trust", "linuxdo", "LinuxDOMinimumTrustLevel", "-1"},
		{"bad trust", "linuxdo", "LinuxDOMinimumTrustLevel", "two"},
		{"missing endpoint", "oidc", "oidc.token_endpoint", ""},
		{"insecure endpoint", "oidc", "oidc.user_info_endpoint", "http://id.test/userinfo"},
		{"credentials in endpoint", "oidc", "oidc.authorization_endpoint", "https://user:pass@id.test/auth"},
		{"fragment in endpoint", "oidc", "oidc.authorization_endpoint", "https://id.test/auth#frag"},
		{"invalid public URL", "github", "ServerAddress", "https://codego.test?query=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := builtinSettings()
			settings[tc.key] = tc.value
			if _, err := buildBuiltinOAuthProvider(tc.provider, "", settings); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid configuration accepted: %v", err)
			}
		})
	}
	settings := builtinSettings()
	settings["GitHubOAuthEnabled"] = "false"
	if _, err := buildBuiltinOAuthProvider("github", "", settings); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled provider accepted: %v", err)
	}
	if _, err := buildBuiltinOAuthProvider("unlisted", "", settings); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown provider accepted: %v", err)
	}
}

func TestBuiltinOAuthImportedScalarDecoding(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`"client-with-quotes"`, "client-with-quotes"}, {`true`, "true"}, {`"false"`, "false"},
		{`12345678901234567890`, "12345678901234567890"}, {`"secret\nline"`, "secret\nline"},
	} {
		value, err := decodeBuiltinOAuthValue([]byte(tc.raw))
		if err != nil || value != tc.want {
			t.Fatalf("scalar decode failed: %v", err)
		}
	}
	for _, raw := range []string{`null`, `{}`, `[]`, `"incomplete`, `1 true`} {
		if _, err := decodeBuiltinOAuthValue([]byte(raw)); err == nil {
			t.Fatalf("malformed scalar accepted: %s", raw)
		}
	}
	if !validBuiltinOAuthURL("http://localhost:3002/auth") || validBuiltinOAuthURL("http://[::1]:3002/auth") || validBuiltinOAuthURL("https://example.test/auth#"+strings.Repeat("x", 3)) {
		t.Fatal("endpoint boundary validation failed")
	}
}

func TestBuiltinOAuthExplicitOverridesSkipStoredSettings(t *testing.T) {
	c, _ := testControl(t)
	c.cfg.OAuth = map[string]OAuthProvider{}
	for _, name := range builtinOAuthNames {
		c.cfg.OAuth[name] = OAuthProvider{ClientID: "explicit-" + name}
	}
	// No pool: an explicit override must not read old or malformed stored settings.
	providers, err := c.builtinOAuthProviders(context.Background())
	if err != nil || len(providers) != 0 {
		t.Fatalf("overrides read or advertised old settings: %v", err)
	}
	p, err := c.builtinOAuthProvider(context.Background(), "github")
	if err != nil || p.ClientID != "explicit-github" {
		t.Fatalf("override not honored: %v", err)
	}
}
