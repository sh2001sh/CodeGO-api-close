package main

import (
	"encoding/base64"
	"testing"
)

func TestConfigurationRejectsMissingAndMalformedSecrets(t *testing.T) {
	for _, name := range []string{"V3_PUBLIC_URL", "V3_SESSION_SECRET", "V3_SECRET_KEY", "V3_OAUTH_PROVIDERS", "V3_DISABLE_REGISTRATION", "V3_SMTP_ADDR", "V3_SMTP_USERNAME", "V3_SMTP_PASSWORD", "V3_SMTP_FROM", "V3_SMTP_IMPLICIT_TLS", "OIDC_ISSUER", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "OIDC_REDIRECT_URI", "OIDC_SIGNING_KEY_ID", "OIDC_SIGNING_PRIVATE_KEY_BASE64", "OIDC_SIGNING_PRIVATE_KEY_FILE"} {
		t.Setenv(name, "")
	}
	if _, err := configFromEnv(); err == nil {
		t.Fatal("missing public origin accepted")
	}
	t.Setenv("V3_PUBLIC_URL", "https://codego.test")
	if _, err := configFromEnv(); err == nil {
		t.Fatal("missing session secret accepted")
	}
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("V3_SESSION_SECRET", key)
	t.Setenv("V3_SECRET_KEY", key)
	t.Setenv("V3_TRUSTED_PROXY_CIDRS", "invalid")
	if _, err := configFromEnv(); err == nil {
		t.Fatal("invalid trusted proxy CIDR accepted")
	}
	t.Setenv("V3_TRUSTED_PROXY_CIDRS", "10.0.0.0/8, 127.0.0.1/32")
	if cfg, err := configFromEnv(); err != nil || len(cfg.TrustedProxies) != 2 {
		t.Fatalf("valid trusted proxy CIDRs rejected: %v", err)
	}
	t.Setenv("V3_TRUSTED_PROXY_CIDRS", "")
	if _, err := configFromEnv(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OIDC_ISSUER", "https://codego.test")
	if _, err := configFromEnv(); err == nil {
		t.Fatal("partial OIDC configuration accepted")
	}
	t.Setenv("OIDC_ISSUER", "")
	t.Setenv("V3_SMTP_ADDR", "smtp.test:465")
	if _, err := configFromEnv(); err == nil {
		t.Fatal("incomplete SMTP configuration accepted")
	}
	t.Setenv("V3_SMTP_FROM", "service@codego.test")
	t.Setenv("V3_SMTP_IMPLICIT_TLS", "true")
	if cfg, err := configFromEnv(); err != nil || cfg.SMTP == nil || !cfg.SMTP.ImplicitTLS {
		t.Fatalf("valid SMTP configuration rejected: %v", err)
	}
	t.Setenv("V3_SMTP_PASSWORD", "sensitive-value")
	if _, err := configFromEnv(); err == nil {
		t.Fatal("unpaired SMTP credential accepted")
	}
}
