package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

var keyOnce sync.Once
var testKey *rsa.PrivateKey
var keyErr error

func testConfig(t *testing.T) Config {
	t.Helper()
	keyOnce.Do(func() { testKey, keyErr = rsa.GenerateKey(rand.Reader, 2048) })
	if keyErr != nil {
		t.Fatal(keyErr)
	}
	return Config{Issuer: "https://codego.test", ClientID: "nodebb", ClientSecret: strings.Repeat("secret", 8),
		RedirectURI: "https://community.test/auth/codego/callback", PrivateKey: testKey}
}

func unitServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(&pgxpool.Pool{}, testConfig(t), func(*http.Request) (int64, error) {
		return 0, errors.New("no session")
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestConfigurationBoundaries(t *testing.T) {
	if _, err := New(nil, Config{}, nil, nil); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled configuration: %v", err)
	}
	cases := []struct {
		name   string
		change func(*Config)
	}{
		{"missing client", func(c *Config) { c.ClientID = "" }},
		{"short secret", func(c *Config) { c.ClientSecret = "short" }},
		{"missing key", func(c *Config) { c.PrivateKey = nil }},
		{"insecure issuer", func(c *Config) { c.Issuer = "http://example.com" }},
		{"issuer credentials", func(c *Config) { c.Issuer = "https://user:pass@example.com" }},
		{"issuer query", func(c *Config) { c.Issuer = "https://example.com?evil=true" }},
		{"redirect fragment", func(c *Config) { c.RedirectURI += "#fragment" }},
		{"redirect reserved parameter", func(c *Config) { c.RedirectURI += "?code=evil" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := testConfig(t)
			tc.change(&c)
			if _, err := normalize(c); err == nil || errors.Is(err, ErrDisabled) {
				t.Fatalf("invalid partial configuration accepted: %v", err)
			}
		})
	}
	for _, raw := range []string{"http://localhost:55562", "http://127.0.0.1:55562", "http://[::1]:55562"} {
		if err := validateURL(raw); err != nil {
			t.Fatalf("loopback rejected: %v", err)
		}
	}
	if _, err := New(nil, testConfig(t), nil, nil); err == nil {
		t.Fatal("missing dependencies accepted")
	}
}

func TestLoadConfigFailClosed(t *testing.T) {
	for _, name := range []string{"OIDC_ISSUER", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "OIDC_REDIRECT_URI", "OIDC_SIGNING_KEY_ID", "OIDC_SIGNING_PRIVATE_KEY_BASE64", "OIDC_SIGNING_PRIVATE_KEY_FILE"} {
		t.Setenv(name, "")
	}
	if _, err := LoadConfig(); !errors.Is(err, ErrDisabled) {
		t.Fatalf("empty environment: %v", err)
	}
	t.Setenv("OIDC_ISSUER", "https://codego.test")
	if _, err := LoadConfig(); err == nil || errors.Is(err, ErrDisabled) {
		t.Fatalf("partial configuration silently disabled: %v", err)
	}
	c := testConfig(t)
	t.Setenv("OIDC_CLIENT_ID", c.ClientID)
	t.Setenv("OIDC_CLIENT_SECRET", c.ClientSecret)
	t.Setenv("OIDC_REDIRECT_URI", c.RedirectURI)
	key := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(c.PrivateKey)})
	t.Setenv("OIDC_SIGNING_PRIVATE_KEY_BASE64", base64.StdEncoding.EncodeToString(key))
	loaded, err := LoadConfig()
	if err != nil || loaded.PrivateKey.N.Cmp(c.PrivateKey.N) != 0 || loaded.KeyID != "codego-oidc-1" {
		t.Fatalf("valid environment not loaded: %v", err)
	}
	t.Setenv("OIDC_SIGNING_PRIVATE_KEY_BASE64", "invalid-base64")
	if _, err = LoadConfig(); err == nil {
		t.Fatal("invalid key accepted")
	}
}
