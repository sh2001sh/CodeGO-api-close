package gateway

import (
	"errors"
	"regexp"
	"strings"

	"golang.org/x/net/http/httpguts"
)

func validOverrideHeaderName(name string) bool { return httpguts.ValidHeaderFieldName(name) }

// Client authentication and upstream account selection are never passthrough.
// Administrator-configured literal values remain trusted channel credentials.
func unsafeOverrideClientHeader(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(name, "x-owner-") || strings.HasPrefix(name, "x-account-") || strings.HasPrefix(name, "x-tenant-") || name == "x-organization-id" || name == "x-project-id" || name == "x-channel-owner-id" {
		return true
	}
	switch name {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade", "cookie", "set-cookie", "host", "content-length", "accept-encoding", "authorization", "x-api-key", "x-goog-api-key", "api-key", "openai-organization", "openai-project", "chatgpt-account-id", "x-account-id", "x-user-id", "x-auth-token", "x-access-token", "x-amz-security-token", "sec-websocket-key", "sec-websocket-version", "sec-websocket-extensions":
		return true
	}
	return false
}

func safeOverrideClientHeaders(req *Request) map[string]any {
	result := map[string]any{}
	for _, source := range []map[string]string{req.PricingHeaders, req.ClientHeaders} {
		for name, value := range source {
			name = normalizeHeaderContextKey(name)
			if unsafeOverrideClientHeader(name) || !validOverrideHeaderName(name) || !httpguts.ValidHeaderFieldValue(value) {
				continue
			}
			result[name] = value
		}
	}
	return result
}

func resolveOverrideHeaders(context map[string]any, req *Request, secret string) (map[string]string, error) {
	source := ensureMapKeyInContext(context, paramOverrideContextHeaderOverride)
	client := safeOverrideClientHeaders(req)
	result := map[string]string{}
	if err := applyWildcardAndRegexHeaderOverrides(source, client, result); err != nil {
		return nil, err
	}
	if err := applyLiteralHeaderOverrides(source, client, secret, result); err != nil {
		return nil, err
	}
	for name := range ensureMapKeyInContext(context, paramOverrideDeletedHeaders) {
		delete(result, name)
	}
	return result, nil
}

// applyWildcardAndRegexHeaderOverrides handles the "*" and "re:"/"regex:"
// source entries, copying matching client headers straight into result.
func applyWildcardAndRegexHeaderOverrides(source map[string]any, client map[string]any, result map[string]string) error {
	for name := range source {
		var pattern string
		switch {
		case name == "*":
			for key, value := range client {
				result[key] = value.(string)
			}
			continue
		case strings.HasPrefix(name, "regex:"):
			pattern = strings.TrimPrefix(name, "regex:")
		case strings.HasPrefix(name, "re:"):
			pattern = strings.TrimPrefix(name, "re:")
		default:
			continue
		}
		if pattern == "" {
			return errors.New("empty header regex")
		}
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			return errors.New("invalid header regex")
		}
		for key, value := range client {
			if re.MatchString(key) {
				result[key] = value.(string)
			}
		}
	}
	return nil
}

// applyLiteralHeaderOverrides handles the plain header-name source entries,
// resolving {client_header:...} and {api_key} placeholders before writing
// into result.
func applyLiteralHeaderOverrides(source map[string]any, client map[string]any, secret string, result map[string]string) error {
	for name, raw := range source {
		if name == "*" || strings.HasPrefix(name, "re:") || strings.HasPrefix(name, "regex:") {
			continue
		}
		if !validOverrideHeaderName(name) {
			return errors.New("invalid header name")
		}
		value, ok := raw.(string)
		if !ok {
			return errors.New("header override must be text")
		}
		value, include, err := resolveOverridePlaceholder(value, client, secret)
		if err != nil {
			return err
		}
		if !include {
			continue
		}
		if !httpguts.ValidHeaderFieldValue(value) {
			return errors.New("invalid header value")
		}
		result[name] = value
	}
	return nil
}

func resolveOverridePlaceholder(value string, client map[string]any, secret string) (string, bool, error) {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "{client_header:") {
		name, ok := strings.CutSuffix(strings.TrimPrefix(trimmed, "{client_header:"), "}")
		if !ok || strings.ContainsAny(name, "{}") || !validOverrideHeaderName(strings.TrimSpace(name)) {
			return "", false, errors.New("invalid client_header placeholder")
		}
		name = normalizeHeaderContextKey(name)
		if unsafeOverrideClientHeader(name) {
			return "", false, errors.New("client authentication header cannot be forwarded")
		}
		value, _ := client[name].(string)
		return value, strings.TrimSpace(value) != "", nil
	}
	value = strings.ReplaceAll(value, "{api_key}", secret)
	return value, strings.TrimSpace(value) != "", nil
}
