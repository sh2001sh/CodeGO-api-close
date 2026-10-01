package baidu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type tokenEntry struct {
	token string
	until time.Time
}

type tokenCall struct {
	done  chan struct{}
	token string
	err   error
}

// TokenCache coalesces refreshes and never serves an expired token.
// Its zero value is ready for use and it can be shared by Provider values.
type TokenCache struct {
	mu       sync.Mutex
	entries  map[string]tokenEntry
	inflight map[string]*tokenCall
}

var defaultCache TokenCache
var defaultClient = &http.Client{Timeout: 15 * time.Second}

func (p Provider) accessToken(ctx context.Context, secret string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	parts := strings.Split(secret, "|")
	if len(parts) == 1 && parts[0] != "" {
		return parts[0], nil
	}
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", errors.New("baidu: credential must be a token or apiKey|secret")
	}
	endpoint := p.TokenURL
	if endpoint == "" {
		endpoint = "https://aip.baidubce.com/oauth/2.0/token"
	}
	cache := p.Cache
	if cache == nil {
		cache = &defaultCache
	}
	key := endpoint + "\x00" + secret
	cache.mu.Lock()
	if entry := cache.entries[key]; time.Now().Before(entry.until) {
		cache.mu.Unlock()
		return entry.token, nil
	}
	if call := cache.inflight[key]; call != nil {
		cache.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-call.done:
			return call.token, call.err
		}
	}
	if cache.inflight == nil {
		cache.inflight = make(map[string]*tokenCall)
	}
	call := &tokenCall{done: make(chan struct{})}
	cache.inflight[key] = call
	cache.mu.Unlock()
	entry, err := p.exchange(ctx, endpoint, parts)
	cache.mu.Lock()
	if err == nil {
		if cache.entries == nil {
			cache.entries = make(map[string]tokenEntry)
		}
		cache.entries[key] = entry
	}
	call.token, call.err = entry.token, err
	delete(cache.inflight, key)
	close(call.done)
	cache.mu.Unlock()
	return entry.token, err
}

func (p Provider) exchange(ctx context.Context, endpoint string, parts []string) (tokenEntry, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return tokenEntry{}, errors.New("baidu: invalid OAuth endpoint")
	}
	q := u.Query()
	q.Set("grant_type", "client_credentials")
	q.Set("client_id", parts[0])
	q.Set("client_secret", parts[1])
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return tokenEntry{}, errors.New("baidu: could not build OAuth request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := p.Client
	if client == nil {
		client = defaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err // URL contains credentials; do not include it in errors.
		}
		return tokenEntry{}, fmt.Errorf("baidu: OAuth transport failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return tokenEntry{}, errors.New("baidu: could not read OAuth response")
	}
	var response struct {
		Token string `json:"access_token"`
		TTL   int64  `json:"expires_in"`
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &response) != nil {
		return tokenEntry{}, errors.New("baidu: invalid OAuth response JSON")
	}
	if resp.StatusCode != http.StatusOK || response.Error != "" {
		return tokenEntry{}, upstream("oauth_failed", "baidu: OAuth token exchange rejected")
	}
	if response.Token == "" || response.TTL < 1 || response.TTL > 365*24*3600 {
		return tokenEntry{}, upstream("oauth_failed", "baidu: OAuth returned no token or invalid expiry")
	}
	lifetime := time.Duration(response.TTL) * time.Second
	margin := min(time.Minute, lifetime/10)
	return tokenEntry{token: response.Token, until: time.Now().Add(lifetime - margin)}, nil
}
