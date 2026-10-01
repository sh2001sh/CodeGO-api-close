package channelmarket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

type ProbeRequest struct{ Provider, BaseURL, Secret, Model string }
type ModelTest struct {
	Model     string    `json:"model"`
	Listed    bool      `json:"listed"`
	Status    string    `json:"status"`
	LatencyMS int64     `json:"latency_ms"`
	Error     string    `json:"error,omitempty"`
	TestedAt  time.Time `json:"tested_at"`
}

func publicAddress(ip netip.Addr) bool {
	return httpx.PublicUpstreamAddress(ip)
}

// PublicDialContext resolves and pins only public IPs at each connection,
// closing the DNS rebinding hole after user-controlled URL validation.
func PublicDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrInvalid
	}
	if port != "443" && port != "8443" {
		return nil, ErrInvalid
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, ErrUnavailable
	}
	for _, ip := range addresses {
		if !publicAddress(ip) {
			return nil, ErrInvalid
		}
	}
	dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	for _, ip := range addresses {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, ErrUnavailable
}
func validUpstream(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if ip, e := netip.ParseAddr(u.Hostname()); e == nil && !publicAddress(ip) {
		return false
	}
	return u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Port() == "" || u.Port() == "443" || u.Port() == "8443")
}

func probe(ctx context.Context, p ProbeRequest) (ModelTest, error) {
	result := ModelTest{Model: p.Model, Status: "failed", TestedAt: time.Now()}
	if !validUpstream(p.BaseURL) {
		return result, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	secret, err := upstreamSecret(p.Secret)
	if err != nil {
		return result, err
	}
	req, err := buildProbeRequest(ctx, p, secret)
	if err != nil {
		return result, err
	}
	data, latency, err := sendProbeRequest(req)
	result.LatencyMS = latency
	if err != nil {
		return result, err
	}
	valid, parseErr := probeResponseValid(data)
	if parseErr != nil {
		return result, parseErr
	}
	if !valid {
		return result, errors.New("channelmarket: empty inference response")
	}
	result.Status = "passed"
	return result, nil
}

// buildProbeRequest constructs the provider-specific probe HTTP request
// (path, body shape and auth header) for p.
func buildProbeRequest(ctx context.Context, p ProbeRequest, secret string) (*http.Request, error) {
	base := strings.TrimRight(p.BaseURL, "/")
	path := "/v1/chat/completions"
	body := map[string]any{"model": p.Model, "messages": []map[string]string{{"role": "user", "content": "Reply OK."}}, "max_tokens": 16, "stream": false}
	switch p.Provider {
	case "openai", "openai_compatible":
		if strings.HasSuffix(base, "/v1") {
			path = "/chat/completions"
		}
	case "codex", "responses":
		path = "/v1/responses"
		body = map[string]any{"model": p.Model, "input": "Reply OK.", "max_output_tokens": 16, "stream": false}
		if strings.HasSuffix(base, "/v1") {
			path = "/responses"
		}
	case "anthropic", "claude":
		path = "/v1/messages"
	case "azure", "azure_openai":
		path = "/openai/deployments/" + url.PathEscape(p.Model) + "/chat/completions?api-version=2025-04-01-preview"
	case "gemini":
		path = "/v1beta/models/" + url.PathEscape(p.Model) + ":generateContent"
		body = map[string]any{"contents": []any{map[string]any{"parts": []any{map[string]string{"text": "Reply OK."}}}}, "generationConfig": map[string]int{"maxOutputTokens": 16}}
	default:
		return nil, ErrInvalid
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	switch p.Provider {
	case "anthropic", "claude":
		req.Header.Set("x-api-key", secret)
		req.Header.Set("anthropic-version", "2023-06-01")
	case "gemini":
		req.Header.Set("x-goog-api-key", secret)
	case "azure", "azure_openai":
		req.Header.Set("api-key", secret)
	default:
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	return req, nil
}

// sendProbeRequest issues the probe request over a pinned-DNS transport and
// returns the (size-capped) response body and observed latency in milliseconds.
func sendProbeRequest(req *http.Request) ([]byte, int64, error) {
	transport := &http.Transport{DialContext: PublicDialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, ForceAttemptHTTP2: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	latency := time.Since(start).Milliseconds()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, latency, errors.New("channelmarket: upstream rejected probe")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil {
		return nil, latency, err
	}
	if len(data) > 2<<20 {
		return nil, latency, ErrInvalid
	}
	return data, latency, nil
}

// probeResponseValid reports whether data decodes as a known provider
// response shape carrying non-empty inference content. A decode failure is
// returned as ErrInvalid, matching the original inline behavior.
func probeResponseValid(data []byte) (bool, error) {
	var message struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Output []json.RawMessage `json:"output"`
	}
	if json.Unmarshal(data, &message) != nil {
		return false, ErrInvalid
	}
	valid := len(message.Content) > 0 || len(message.Candidates) > 0 || len(message.Output) > 0
	for _, c := range message.Choices {
		if len(c.Message.Content) > 0 && string(c.Message.Content) != "null" && string(c.Message.Content) != `""` {
			valid = true
		}
	}
	return valid, nil
}

func upstreamSecret(raw string) (string, error) {
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		return raw, nil
	}
	var token struct {
		Access string `json:"access_token"`
	}
	if json.Unmarshal([]byte(raw), &token) != nil || token.Access == "" {
		return "", ErrInvalid
	}
	return token.Access, nil
}
