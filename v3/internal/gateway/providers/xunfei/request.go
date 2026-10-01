// Package xunfei adapts Spark's signed native WebSocket chat protocol.
package xunfei

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

const ID = "xunfei"
const maxFrame = 1 << 20
const maxOutput = 16 << 20

type Provider struct{}

var versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+$`)

func (Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req.Protocol != gateway.ProtocolOpenAIChat {
		return nil, errors.New("xunfei: native endpoint requires Chat protocol")
	}
	credentials := strings.Split(target.Secret, "|")
	if len(credentials) != 3 || credentials[0] == "" || credentials[1] == "" || credentials[2] == "" || strings.ContainsAny(credentials[2], "\"\r\n") {
		return nil, errors.New("xunfei: invalid credential format")
	}
	model := target.UpstreamModel
	if model == "" {
		model = req.Model
	}
	configuredVersion := ""
	if value, exists := target.Settings["api_version"]; exists {
		var valid bool
		configuredVersion, valid = value.(string)
		if !valid || (configuredVersion != "" && !versionPattern.MatchString(configuredVersion)) {
			return nil, &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: "invalid_channel_api_version", Message: "xunfei: invalid channel API version"}
		}
	}
	if requested := req.ClientHeaders["X-Spark-Api-Version"]; requested != "" {
		if !versionPattern.MatchString(requested) {
			return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "invalid_api_version", Message: "xunfei: invalid API version"}
		}
		configuredVersion = requested
	}
	endpoint, version, err := endpointURL(target.BaseURL, model, configuredVersion)
	if err != nil {
		return nil, err
	}
	body, err := nativeRequest(req.Body, credentials[0], model, version)
	if err != nil {
		return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "unsupported_request", Message: err.Error()}
	}
	signURL(endpoint, credentials[2], credentials[1], time.Now())
	// HTTP(S) permits the gateway's request lifecycle; RoundTrip restores WS(S).
	if endpoint.Scheme == "ws" {
		endpoint.Scheme = "http"
	} else {
		endpoint.Scheme = "https"
	}
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("xunfei: invalid upstream request")
	}
	out.Header.Set("Content-Type", "application/json")
	return out, nil
}

func endpointURL(base, model, configuredVersion string) (*url.URL, string, error) {
	if base == "" {
		base = "wss://spark-api.xf-yun.com"
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, "", errors.New("xunfei: invalid endpoint")
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return nil, "", errors.New("xunfei: unsupported endpoint scheme")
	}
	version := configuredVersion
	if version == "" {
		version = u.Query().Get("api-version")
	}
	if version == "" && strings.HasSuffix(u.Path, "/chat") {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 && versionPattern.MatchString(parts[len(parts)-2]) {
			version = parts[len(parts)-2]
		}
	}
	if version == "" {
		if parts := strings.Split(model, "-"); len(parts) == 2 {
			version = parts[1]
		}
	}
	if version == "" {
		version = "v1.1"
	}
	if !versionPattern.MatchString(version) {
		return nil, "", errors.New("xunfei: invalid API version")
	}
	setEndpointVersion(u, version)
	query := u.Query()
	query.Del("api-version")
	u.RawQuery = query.Encode()
	return u, version, nil
}

// Preserve custom relay prefixes while making a native version path agree
// with its domain. A fixed custom endpoint without a version segment stays
// authoritative; only its native domain parameter changes.
func setEndpointVersion(u *url.URL, version string) {
	if u.Path == "" || u.Path == "/" {
		u.Path, u.RawPath = "/"+version+"/chat", ""
		return
	}
	parts := strings.Split(u.Path, "/")
	if len(parts) < 3 || parts[len(parts)-1] != "chat" || !versionPattern.MatchString(parts[len(parts)-2]) {
		return
	}
	escaped := strings.Split(u.EscapedPath(), "/")
	parts[len(parts)-2] = version
	escaped[len(escaped)-2] = version
	u.Path, u.RawPath = strings.Join(parts, "/"), strings.Join(escaped, "/")
}

func signURL(u *url.URL, apiKey, apiSecret string, now time.Time) {
	date := now.UTC().Format(http.TimeFormat)
	canonical := "host: " + u.Host + "\ndate: " + date + "\nGET " + u.EscapedPath() + " HTTP/1.1"
	mac := hmac.New(sha256.New, []byte(apiSecret))
	_, _ = mac.Write([]byte(canonical))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	auth := fmt.Sprintf(`hmac username="%s", algorithm="hmac-sha256", headers="host date request-line", signature="%s"`, apiKey, signature)
	q := u.Query()
	q.Set("host", u.Host)
	q.Set("date", date)
	q.Set("authorization", base64.StdEncoding.EncodeToString([]byte(auth)))
	u.RawQuery = q.Encode()
}

func nativeRequest(body []byte, appID, model, version string) ([]byte, error) {
	root := gjson.ParseBytes(body)
	if len(body) > maxFrame || !gjson.ValidBytes(body) || !root.IsObject() || !root.Get("messages").IsArray() || len(root.Get("messages").Array()) == 0 {
		return nil, errors.New("xunfei: invalid or oversized Chat request")
	}
	if err := validateUnsupportedFields(root); err != nil {
		return nil, err
	}
	messages, err := convertSparkMessages(root.Get("messages"), model)
	if err != nil {
		return nil, err
	}
	chat, err := sparkChatParams(root, version)
	if err != nil {
		return nil, err
	}
	result, err := json.Marshal(map[string]any{"header": map[string]string{"app_id": appID}, "parameter": map[string]any{"chat": chat}, "payload": map[string]any{"message": map[string]any{"text": messages}}})
	if err == nil && len(result) > maxFrame {
		err = errors.New("xunfei: native request exceeds limit")
	}
	return result, err
}

func validateUnsupportedFields(root gjson.Result) error {
	for _, field := range []string{"tools", "tool_choice", "functions", "function_call", "response_format", "audio", "modalities"} {
		if value := root.Get(field); value.Exists() && value.Type != gjson.Null {
			return errors.New("xunfei: unsupported request feature " + field)
		}
	}
	if n := root.Get("n"); n.Exists() && (n.Type != gjson.Number || n.Float() != 1) {
		return errors.New("xunfei: multiple completions are unsupported")
	}
	return nil
}

// convertSparkMessages validates and converts the OpenAI-style messages
// array to Spark's native shape, splitting a leading system message into a
// user/assistant pair for models that cannot take a system role directly.
func convertSparkMessages(messages gjson.Result, model string) ([]map[string]string, error) {
	out := []map[string]string{}
	for _, m := range messages.Array() {
		role := m.Get("role").Str
		if role != "system" && role != "user" && role != "assistant" {
			return nil, errors.New("xunfei: unsupported message role")
		}
		if m.Get("tool_calls").Exists() || m.Get("function_call").Exists() {
			return nil, errors.New("xunfei: tool messages are unsupported")
		}
		content := m.Get("content")
		text := ""
		if content.Type == gjson.String {
			text = content.Str
		} else if content.IsArray() {
			for _, part := range content.Array() {
				if part.Get("type").Str != "text" || part.Get("text").Type != gjson.String {
					return nil, errors.New("xunfei: only text content is supported")
				}
				text += part.Get("text").Str
			}
		} else {
			return nil, errors.New("xunfei: invalid message content")
		}
		if role == "system" && !strings.HasSuffix(model, "3.5") {
			out = append(out, map[string]string{"role": "user", "content": text}, map[string]string{"role": "assistant", "content": "Okay"})
		} else {
			out = append(out, map[string]string{"role": role, "content": text})
		}
	}
	return out, nil
}

func sparkChatParams(root gjson.Result, version string) (map[string]any, error) {
	domain := map[string]string{"v1.1": "lite", "v2.1": "generalv2", "v3.1": "generalv3", "v3.5": "generalv3.5", "v4.0": "4.0Ultra"}[version]
	if domain == "" {
		domain = "general" + version
	}
	chat := map[string]any{"domain": domain}
	for _, field := range []string{"temperature", "top_k"} {
		if value := root.Get(field); value.Exists() {
			if value.Type != gjson.Number || value.Float() < 0 || (field == "top_k" && float64(value.Int()) != value.Float()) {
				return nil, errors.New("xunfei: invalid numeric parameter")
			}
			chat[field] = value.Value()
		}
	}
	if !root.Get("top_k").Exists() && root.Get("n").Exists() {
		chat["top_k"] = root.Get("n").Int()
	}
	tokens := root.Get("max_completion_tokens")
	if !tokens.Exists() {
		tokens = root.Get("max_tokens")
	}
	if tokens.Exists() {
		if tokens.Type != gjson.Number || tokens.Int() < 0 || float64(tokens.Int()) != tokens.Float() {
			return nil, errors.New("xunfei: invalid maximum tokens")
		}
		chat["max_tokens"] = tokens.Int()
	}
	return chat, nil
}
