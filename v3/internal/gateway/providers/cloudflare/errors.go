package cloudflare

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func parseResponse(data []byte) (nativeResponse, *gateway.UpstreamError) {
	var in nativeResponse
	if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' || json.Unmarshal(data, &in) != nil {
		// Unmarshal may fill some fields before rejecting a type mismatch.
		// Those partially decoded counts must never become authoritative usage.
		return nativeResponse{}, invalidResponse("invalid Cloudflare JSON response")
	}
	if upstream := nativeFailure(in); upstream != nil {
		return in, upstream
	}
	if len(in.Result) != 0 {
		outerUsage := in.Usage
		if len(bytes.TrimSpace(in.Result)) == 0 || bytes.TrimSpace(in.Result)[0] != '{' {
			return in, invalidResponse("Cloudflare result must be an object")
		}
		// Unmarshal into a fresh value: outer fields must not mask result errors.
		var result nativeResponse
		if json.Unmarshal(in.Result, &result) != nil {
			return in, invalidResponse("invalid Cloudflare result")
		}
		in = result
		if in.Usage == nil {
			in.Usage = outerUsage
		}
		if upstream := nativeFailure(in); upstream != nil {
			return in, upstream
		}
	}
	if calls := bytes.TrimSpace(in.ToolCalls); len(calls) != 0 && !bytes.Equal(calls, []byte("null")) && !bytes.Equal(calls, []byte("[]")) {
		return in, invalidResponse("Cloudflare native tool calls are unsupported by this Chat adapter")
	}
	return in, nil
}

func nativeFailure(in nativeResponse) *gateway.UpstreamError {
	if len(in.Errors) != 0 {
		return cloudflareError(in.Errors[0])
	}
	if raw := bytes.TrimSpace(in.Error); len(raw) != 0 && !bytes.Equal(raw, []byte("null")) {
		var message string
		if json.Unmarshal(raw, &message) == nil {
			return cloudflareError(nativeError{Message: message})
		}
		var failure nativeError
		if json.Unmarshal(raw, &failure) == nil && raw[0] == '{' {
			return cloudflareError(failure)
		}
		return invalidResponse("Cloudflare returned a malformed error")
	}
	if in.Success != nil && !*in.Success {
		return cloudflareError(nativeError{Message: "Cloudflare request failed"})
	}
	return nil
}

func cloudflareError(in nativeError) *gateway.UpstreamError {
	code := "cloudflare_error"
	var text string
	if json.Unmarshal(in.Code, &text) == nil && text != "" {
		code = "cloudflare_" + text
	} else if len(in.Code) != 0 && string(in.Code) != "null" {
		var number json.Number
		if json.Unmarshal(in.Code, &number) == nil {
			code = "cloudflare_" + number.String()
		}
	}
	message := in.Message
	if message == "" {
		message = "Cloudflare request failed"
	}
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: code, Message: message}
}

func invalidResponse(message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: "invalid_response", Message: message}
}

func responseError(code, message string) gateway.Event {
	return gateway.Event{Kind: gateway.EventError, Err: &gateway.UpstreamError{
		Status: http.StatusBadGateway, Type: "upstream_error", Code: code, Message: message,
	}}
}
