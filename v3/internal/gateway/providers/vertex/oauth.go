package vertex

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type tokenEntry struct {
	token   string
	expires time.Time
	wait    chan struct{}
}

type tokenCache struct {
	mu      sync.Mutex
	entries map[[32]byte]*tokenEntry
}

var sharedTokens = &tokenCache{entries: make(map[[32]byte]*tokenEntry)}
var defaultOAuthClient = &http.Client{Timeout: 30 * time.Second, CheckRedirect: rejectRedirect}

func rejectRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func (p Provider) accessToken(ctx context.Context, secret string, c Credentials, proxy string, targets ...gateway.Target) (string, error) {
	endpoint := p.TokenEndpoint
	if endpoint == "" {
		endpoint = c.TokenURI
	}
	if endpoint == "" {
		endpoint = defaultTokenEndpoint
	}
	if _, err := endpointURL(endpoint); err != nil {
		return "", err
	}
	identity := ""
	if len(targets) != 0 {
		t := targets[0]
		identity = fmt.Sprintf("%d:%d:%s:%s", t.ChannelID, t.CredentialID, t.Fingerprint.UserAgent, t.Fingerprint.TLSProfile)
	}
	key := sha256.Sum256([]byte(secret + "\x00" + endpoint + "\x00" + proxy + "\x00" + identity))
	cache := p.tokens
	if cache == nil {
		cache = sharedTokens
	}
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		cache.mu.Lock()
		entry := cache.entries[key]
		if entry != nil && entry.token != "" && p.now().Before(entry.expires.Add(-time.Minute)) {
			token := entry.token
			cache.mu.Unlock()
			return token, nil
		}
		if entry != nil && entry.wait != nil {
			wait := entry.wait
			cache.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-wait:
				continue
			}
		}
		entry = &tokenEntry{wait: make(chan struct{})}
		cache.entries[key] = entry
		cache.mu.Unlock()
		token, expires, err := p.exchangeToken(ctx, c, endpoint, proxy, targets...)
		cache.mu.Lock()
		if err == nil {
			entry.token, entry.expires = token, expires
		} else {
			delete(cache.entries, key)
		}
		close(entry.wait)
		entry.wait = nil
		cache.mu.Unlock()
		return token, err
	}
}

func (p Provider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p Provider) exchangeToken(ctx context.Context, c Credentials, endpoint, proxy string, targets ...gateway.Target) (string, time.Time, error) {
	privateKey, err := parsePrivateKey(c.PrivateKey)
	if err != nil {
		return "", time.Time{}, err
	}
	now := p.now()
	assertion := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": c.ClientEmail, "scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud": endpoint, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	if c.PrivateKeyID != "" {
		assertion.Header["kid"] = c.PrivateKeyID
	}
	signed, err := assertion.SignedString(privateKey)
	if err != nil {
		return "", time.Time{}, errors.New("vertex: cannot sign service-account assertion")
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {signed}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, errors.New("vertex: cannot build token exchange")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client, err := p.tokenClient(ctx, proxy, targets)
	if err != nil {
		return "", time.Time{}, err
	}
	if p.HTTPClient == nil && p.Clients == nil && proxy != "" {
		defer client.CloseIdleConnections()
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return "", time.Time{}, ctx.Err()
		}
		return "", time.Time{}, errors.New("vertex: token exchange transport failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("vertex: token exchange returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return "", time.Time{}, errors.New("vertex: cannot read token response")
	}
	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if json.Unmarshal(data, &result) != nil || result.AccessToken == "" || result.ExpiresIn <= 0 || result.ExpiresIn > 86400 ||
		(result.TokenType != "" && !strings.EqualFold(result.TokenType, "Bearer")) || strings.ContainsAny(result.AccessToken, "\r\n") {
		return "", time.Time{}, errors.New("vertex: invalid token response")
	}
	return result.AccessToken, now.Add(time.Duration(result.ExpiresIn) * time.Second), nil
}

func (p Provider) tokenClient(ctx context.Context, proxy string, targets []gateway.Target) (*http.Client, error) {
	target := gateway.Target{ProxyURL: proxy}
	if len(targets) != 0 {
		target = targets[0]
	}
	if p.Clients != nil {
		client, err := p.Clients(ctx, target)
		if errors.Is(err, context.Canceled) {
			return nil, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		if err != nil || client == nil || client.Transport == nil && target.Fingerprint.TLSProfile != "" {
			return nil, &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error",
				Code: "invalid_channel_configuration", Message: "vertex: selected OAuth client is unavailable"}
		}
		copyClient := *client
		copyClient.CheckRedirect = rejectRedirect
		if copyClient.Timeout <= 0 || copyClient.Timeout > 30*time.Second {
			copyClient.Timeout = 30 * time.Second
		}
		return &copyClient, nil
	}
	if target.Fingerprint.TLSProfile != "" && (p.HTTPClient == nil || p.HTTPClient.Transport == nil) {
		return nil, &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error",
			Code: "invalid_channel_configuration", Message: "vertex: custom TLS requires a selected OAuth client"}
	}
	if p.HTTPClient != nil {
		client := *p.HTTPClient
		client.CheckRedirect = rejectRedirect
		return &client, nil
	}
	if proxy == "" {
		return defaultOAuthClient, nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") {
			return nil, errors.New("vertex: invalid token proxy URL")
		}
		transport.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: rejectRedirect}, nil
}
