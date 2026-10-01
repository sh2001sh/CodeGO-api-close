package gateway

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ApplyUpstreamRequest applies channel policy after native protocol conversion.
// Original client bytes, frozen pricing inputs and the reservation stay intact.
func ApplyUpstreamRequest(out *http.Request, req *Request, target Target) error {
	if out == nil || req == nil {
		return invalidOverride("invalid_upstream_request")
	}
	if err := validateOverrideTargetSettings(target); err != nil {
		return err
	}
	if out.Header == nil {
		out.Header = make(http.Header)
	}
	context := buildOverrideContext(req, target, out)
	configureOverrideHeaderDefaults(context, target)

	if err := applyOverrideBody(out, req, target, context); err != nil {
		return err
	}
	return applyOverrideHeadersToRequest(out, req, target, context)
}

// validateOverrideTargetSettings rejects malformed stored channel settings
// before any upstream bytes are touched.
func validateOverrideTargetSettings(target Target) error {
	for _, field := range []string{"force_format", "thinking_to_content", "pass_through_body_enabled"} {
		if _, err := overrideSettingBool(target.Settings, field); err != nil {
			return invalidOverride("invalid_channel_settings")
		}
	}
	for from, to := range target.StatusCodeMapping {
		status, err := strconv.Atoi(from)
		if err != nil || status < 100 || status > 599 || to < 100 || to > 599 {
			return invalidOverride("invalid_status_code_mapping")
		}
	}
	return nil
}

// configureOverrideHeaderDefaults seeds the condition context's header
// override map with the channel's configured overrides and any
// provider-specific implicit headers.
func configureOverrideHeaderDefaults(context map[string]any, target Target) {
	configured := ensureMapKeyInContext(context, paramOverrideContextHeaderOverride)
	for name, value := range target.HeaderOverride {
		configured[normalizeHeaderContextKey(name)] = value
	}
	if target.Provider == "ali" || target.Provider == "dashscope" {
		plugin, _ := target.Settings["plugin"].(string)
		if plugin == "" {
			plugin, _ = target.Settings["api_version"].(string)
		}
		if plugin != "" {
			configured["x-dashscope-plugin"] = plugin
		}
	}
}

// applyOverrideBody rewrites the upstream request body with channel settings
// and param overrides when it is JSON. Non-JSON bodies are left untouched,
// unless param overrides were configured, which is rejected.
func applyOverrideBody(out *http.Request, req *Request, target Target, context map[string]any) error {
	mediaType, _, _ := mime.ParseMediaType(out.Header.Get("Content-Type"))
	isJSON := mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
	if !isJSON {
		if len(target.ParamOverride) > 0 {
			return invalidOverride("param_override_requires_json")
		}
		return nil
	}
	if out.Body == nil {
		return nil
	}

	body, err := readOverrideBody(out)
	if err != nil {
		return invalidOverride("upstream_body_unreadable")
	}
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return invalidOverride("param_override_requires_json_object")
	}
	anthropic := out.Header.Get("Anthropic-Version") != "" || (out.URL != nil && strings.HasSuffix(out.URL.Path, "/messages"))
	body, err = applyOverrideSettings(body, target.Settings, anthropic)
	if err != nil {
		return invalidOverride("invalid_channel_settings")
	}
	body, err = applyParamOverride(body, target.ParamOverride, context)
	if err != nil {
		var intentional *ParamOverrideReturnError
		if errors.As(err, &intentional) {
			return intentional
		}
		return invalidOverride("invalid_param_override")
	}
	if err := out.Body.Close(); err != nil {
		return invalidOverride("upstream_body_unreadable")
	}
	resetOverrideBody(out, body)
	return nil
}

// applyOverrideHeadersToRequest resolves the final header set from the
// condition context and applies deletions and overrides to the outgoing
// request, keeping out.Host in sync with any Host header change.
func applyOverrideHeadersToRequest(out *http.Request, req *Request, target Target, context map[string]any) error {
	headers, err := resolveOverrideHeaders(context, req, target.Secret)
	if err != nil {
		return invalidOverride("invalid_header_override")
	}
	for name := range ensureMapKeyInContext(context, paramOverrideDeletedHeaders) {
		out.Header.Del(name)
		if strings.EqualFold(name, "Host") {
			out.Host = ""
		}
	}
	for name, value := range headers {
		out.Header.Set(name, value)
		if strings.EqualFold(name, "Host") {
			out.Host = value
		}
	}
	return nil
}

// MapUpstreamStatus controls success detection and failure classification.
// Invalid stored mappings are rejected by ApplyUpstreamRequest before dispatch.
func MapUpstreamStatus(status int, target Target) int {
	if mapped, ok := target.StatusCodeMapping[strconv.Itoa(status)]; ok && mapped >= 100 && mapped <= 599 {
		return mapped
	}
	return status
}

func invalidOverride(code string) *UpstreamError {
	return &UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: code, Message: "channel request override could not be applied"}
}

func readOverrideBody(out *http.Request) ([]byte, error) {
	if out.GetBody != nil {
		copyBody, err := out.GetBody()
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(copyBody)
		closeErr := copyBody.Close()
		return body, errors.Join(readErr, closeErr)
	}
	body, err := io.ReadAll(out.Body)
	closeErr := out.Body.Close()
	resetOverrideBody(out, body)
	return body, errors.Join(err, closeErr)
}

func resetOverrideBody(out *http.Request, body []byte) {
	out.Body = io.NopCloser(bytes.NewReader(body))
	out.ContentLength = int64(len(body))
	out.Header.Del("Content-Length")
	out.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
}

func buildOverrideContext(req *Request, target Target, out *http.Request) map[string]any {
	model := target.UpstreamModel
	if model == "" {
		model = req.Model
	}
	context := map[string]any{"model": model, "upstream_model": model, "original_model": req.Model,
		"user_id": req.Principal.UserID, "token_id": req.Principal.KeyID, "key_id": req.Principal.KeyID,
		"user_group": req.Principal.Group, "group": req.Principal.Group, "using_group": target.Group,
		"protocol": req.Protocol, "is_channel_test": false, "retry_index": len(req.Attempts), "is_retry": len(req.Attempts) > 0,
		"retry":                            map[string]any{"index": len(req.Attempts), "is_retry": len(req.Attempts) > 0},
		paramOverrideContextRequestHeaders: safeOverrideClientHeaders(req)}
	if req.Path != "" {
		context["request_path"] = req.Path
	} else if out.URL != nil {
		context["request_path"] = out.URL.Path
	}
	if len(req.Attempts) > 0 {
		previous := req.Attempts[len(req.Attempts)-1].Result
		if previous.Err != nil {
			e := previous.Err
			context["last_error"] = map[string]any{"status_code": e.Status, "message": e.Message, "code": e.Code, "error_code": e.Code, "type": e.Type, "error_type": e.Type, "skip_retry": !previous.Retryable}
			context["last_error_status_code"], context["last_error_message"], context["last_error_code"], context["last_error_type"] = e.Status, e.Message, e.Code, e.Type
		}
	}
	return context
}

func applyParamOverride(data []byte, overrides map[string]any, context map[string]any) ([]byte, error) {
	result := data
	var operations []ParamOperation
	if value, exists := overrides["operations"]; exists {
		var err error
		operations, err = parseOverrideOperations(value)
		if err != nil {
			return nil, err
		}
	}
	for key, value := range overrides {
		if key == "operations" {
			continue
		}
		var err error
		result, err = sjson.SetBytes(result, escapeSjsonLiteralKey(key), value)
		if err != nil {
			return nil, err
		}
	}
	return applyOperations(result, operations, context)
}

func escapeSjsonLiteralKey(key string) string {
	var result strings.Builder
	for _, ch := range key {
		if strings.ContainsRune(".*?\\", ch) {
			result.WriteByte('\\')
		}
		result.WriteRune(ch)
	}
	return result.String()
}
