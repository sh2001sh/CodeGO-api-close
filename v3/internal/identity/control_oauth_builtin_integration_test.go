//go:build pgintegration

package identity

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestBuiltinOAuthSettingsEncryptedSecretsAndSafeListing(t *testing.T) {
	pool, _ := testDeps(t)
	c, _ := testControl(t)
	c.pool = pool
	settings := builtinSettings()
	for key, value := range settings {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		// Legacy importer also encrypts token_endpoint because its key contains token.
		if strings.Contains(strings.ToLower(key), "secret") || strings.Contains(key, "token") {
			encrypted, err := c.encryptKey(string(raw))
			if err != nil {
				t.Fatal(err)
			}
			mustExec(t, pool, `INSERT INTO v3_platform.settings(key,ciphertext,sensitive) VALUES($1,$2,true)`, key, encrypted)
		} else {
			mustExec(t, pool, `INSERT INTO v3_platform.settings(key,value) VALUES($1,$2::jsonb)`, key, raw)
		}
	}
	for _, name := range builtinOAuthNames {
		p, err := c.builtinOAuthProvider(ctx, name)
		if err != nil || p.ClientSecret != settings[map[string]string{"github": "GitHubClientSecret", "linuxdo": "LinuxDOClientSecret", "discord": "discord.client_secret", "oidc": "oidc.client_secret"}[name]] {
			t.Fatalf("encrypted %s configuration failed: %v", name, err)
		}
	}
	providers, err := c.builtinOAuthProviders(ctx)
	if err != nil || len(providers) != 4 {
		t.Fatalf("safe listing failed: %d %v", len(providers), err)
	}
	raw, err := json.Marshal(providers)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "client") {
		t.Fatal("public listing leaked credentials")
	}
	mustExec(t, pool, `UPDATE v3_platform.settings SET value='false' WHERE key='GitHubOAuthEnabled'`)
	if _, err = c.builtinOAuthProvider(ctx, "github"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled provider accepted: %v", err)
	}
	c.cfg.OAuth = map[string]OAuthProvider{"github": {ClientID: "explicit-github"}}
	mustExec(t, pool, `UPDATE v3_platform.settings SET ciphertext='\x00' WHERE key='GitHubClientSecret'`)
	providers, err = c.builtinOAuthProviders(ctx)
	if err != nil || len(providers) != 3 {
		t.Fatalf("explicit override exposed broken stored settings: %d %v", len(providers), err)
	}
	if p, e := c.builtinOAuthProvider(ctx, "github"); e != nil || p.ClientID != "explicit-github" {
		t.Fatalf("explicit override ignored: %v", e)
	}
	mustExec(t, pool, `UPDATE v3_platform.settings SET ciphertext='\x00' WHERE key='oidc.client_secret'`)
	if _, err = c.builtinOAuthProvider(ctx, "oidc"); err == nil || strings.Contains(err.Error(), "oidc-secret") {
		t.Fatalf("tampered secret accepted or exposed: %v", err)
	}
}
