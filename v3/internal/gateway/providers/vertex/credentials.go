package vertex

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/url"
	"strings"
)

const defaultTokenEndpoint = "https://oauth2.googleapis.com/token"

// Credentials accepts the original Google service-account JSON. Region and
// Regions remain a fallback when Target.Settings["api_version"] is absent.
// APIKey also supports Vertex express endpoints without a service account.
type Credentials struct {
	ProjectID    string            `json:"project_id"`
	PrivateKeyID string            `json:"private_key_id"`
	PrivateKey   string            `json:"private_key"`
	ClientEmail  string            `json:"client_email"`
	TokenURI     string            `json:"token_uri"`
	Region       string            `json:"region"`
	Regions      map[string]string `json:"regions"`
	APIKey       string            `json:"api_key"`
	AccessToken  string            `json:"access_token"`
}

func credentials(secret string) (Credentials, error) {
	var c Credentials
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return c, errors.New("vertex: missing credentials")
	}
	if strings.HasPrefix(secret, "{") || strings.HasPrefix(secret, "[") {
		if json.Unmarshal([]byte(secret), &c) != nil {
			return c, errors.New("vertex: invalid credential JSON")
		}
	} else {
		c.APIKey = secret
	}
	if c.APIKey == "" && c.AccessToken == "" && (c.ClientEmail == "" || c.PrivateKey == "" || c.ProjectID == "") {
		return c, errors.New("vertex: service account requires project_id, client_email and private_key")
	}
	methods := 0
	for _, present := range []bool{c.APIKey != "", c.AccessToken != "", c.PrivateKey != ""} {
		if present {
			methods++
		}
	}
	if methods != 1 {
		return c, errors.New("vertex: credentials require exactly one authentication method")
	}
	return c, nil
}

func (c Credentials) region(model string) string {
	if r := c.Regions[model]; r != "" {
		return r
	}
	if r := c.Regions["default"]; r != "" {
		return r
	}
	if c.Region != "" {
		return c.Region
	}
	return "global"
}

func parsePrivateKey(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.ReplaceAll(value, `\n`, "\n")))
	if block == nil {
		return nil, errors.New("vertex: invalid service-account private key")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, errors.New("vertex: service-account private key must be RSA")
}

func endpointURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("vertex: invalid endpoint URL")
	}
	return u, nil
}
