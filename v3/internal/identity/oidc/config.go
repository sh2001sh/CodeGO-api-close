// Package oidc serves CodeGo's confidential-client OpenID Connect provider.
package oidc

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

var ErrDisabled = errors.New("oidc: provider is not configured")

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURI  string
	KeyID        string
	PrivateKey   *rsa.PrivateKey
	Now          func() time.Time
}

// LoadConfig is intended for command assembly; requests never read environment.
func LoadConfig() (Config, error) {
	c := Config{
		Issuer:       strings.TrimRight(strings.TrimSpace(os.Getenv("OIDC_ISSUER")), "/"),
		ClientID:     strings.TrimSpace(os.Getenv("OIDC_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("OIDC_CLIENT_SECRET")),
		RedirectURI:  strings.TrimSpace(os.Getenv("OIDC_REDIRECT_URI")),
		KeyID:        strings.TrimSpace(os.Getenv("OIDC_SIGNING_KEY_ID")),
	}
	encoded := strings.TrimSpace(os.Getenv("OIDC_SIGNING_PRIVATE_KEY_BASE64"))
	path := strings.TrimSpace(os.Getenv("OIDC_SIGNING_PRIVATE_KEY_FILE"))
	if c.empty() && encoded == "" && path == "" {
		return Config{}, ErrDisabled
	}
	var data []byte
	var err error
	switch {
	case encoded != "":
		data, err = base64.StdEncoding.DecodeString(encoded)
	case path != "":
		data, err = os.ReadFile(path)
	default:
		return Config{}, errors.New("oidc: signing private key is required")
	}
	if err != nil {
		return Config{}, errors.New("oidc: cannot load signing private key")
	}
	block, rest := pem.Decode(data)
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 {
		return Config{}, errors.New("oidc: signing key must be a single PEM private key")
	}
	if key, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes); parseErr == nil {
		c.PrivateKey, _ = key.(*rsa.PrivateKey)
	} else {
		c.PrivateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return Config{}, errors.New("oidc: invalid RSA signing private key")
		}
	}
	return normalize(c)
}

func (c Config) empty() bool {
	return c.Issuer == "" && c.ClientID == "" && c.ClientSecret == "" && c.RedirectURI == "" && c.KeyID == "" && c.PrivateKey == nil
}

func normalize(c Config) (Config, error) {
	if c.empty() {
		return Config{}, ErrDisabled
	}
	c.Issuer = strings.TrimRight(c.Issuer, "/")
	if c.ClientID == "" || len(c.ClientID) > 256 || len(c.ClientSecret) < 32 || c.PrivateKey == nil {
		return Config{}, errors.New("oidc: complete client configuration and RSA signing key are required; client secret must have at least 32 bytes")
	}
	for name, raw := range map[string]string{"issuer": c.Issuer, "redirect URI": c.RedirectURI} {
		if err := validateURL(raw); err != nil {
			return Config{}, fmt.Errorf("oidc: invalid %s: %w", name, err)
		}
	}
	u, _ := url.Parse(c.Issuer)
	if u.RawQuery != "" {
		return Config{}, errors.New("oidc: issuer cannot have a query")
	}
	u, _ = url.Parse(c.RedirectURI)
	for _, name := range []string{"code", "state", "error", "error_description"} {
		if u.Query().Has(name) {
			return Config{}, errors.New("oidc: redirect URI contains reserved parameters")
		}
	}
	if c.PrivateKey.N == nil || c.PrivateKey.N.BitLen() < 2048 || c.PrivateKey.Validate() != nil {
		return Config{}, errors.New("oidc: valid RSA signing key of at least 2048 bits is required")
	}
	if c.KeyID == "" {
		c.KeyID = "codego-oidc-1"
	}
	if len(c.KeyID) > 256 {
		return Config{}, errors.New("oidc: signing key ID is too long")
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c, nil
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return errors.New("absolute URL without credentials or fragment required")
	}
	if u.Scheme == "https" {
		return nil
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()) {
		return nil
	}
	return errors.New("HTTPS required except loopback addresses")
}
