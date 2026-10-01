package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

// parseChat reads an OpenAI chat completions body. Only the fields routing
// and billing need are extracted; the body itself is forwarded untouched.
func parseRequest(w http.ResponseWriter, r *http.Request, maxBytes int64, req *Request) *clientError {
	req.Path = r.URL.Path
	req.Protocol = ProtocolOpenAIChat
	switch r.URL.Path {
	case "/v1/responses":
		req.Protocol = ProtocolResponses
	case "/v1/messages":
		req.Protocol = ProtocolAnthropic
	default:
		if r.PathValue("action") != "" {
			req.Protocol = ProtocolGemini
		}
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return errBodyTooLarge
		}
		return errBadBody
	}
	if !gjson.ValidBytes(body) {
		return errBadBody
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return errBadBody
	}
	if clientErr := parseRequestModelAndProtocol(r, root, req); clientErr != nil {
		return clientErr
	}
	req.Body = body
	populateRequestHeaders(r, req)
	return nil
}

// parseRequestModelAndProtocol resolves the wire protocol from the request
// path and, for Gemini's path-encoded action, extracts the model and stream
// flag from the action instead of the body.
func parseRequestModelAndProtocol(r *http.Request, root gjson.Result, req *Request) *clientError {
	req.Protocol = ProtocolOpenAIChat
	switch r.URL.Path {
	case "/v1/responses":
		req.Protocol = ProtocolResponses
	case "/v1/messages":
		req.Protocol = ProtocolAnthropic
	default:
		if action := r.PathValue("action"); action != "" {
			name, method, ok := strings.Cut(action, ":")
			if !ok || name == "" || (method != "generateContent" && method != "streamGenerateContent") {
				return errBadBody
			}
			req.Protocol = ProtocolGemini
			req.Model = name
			req.Stream = method == "streamGenerateContent"
		}
	}
	if req.Protocol != ProtocolGemini {
		model := root.Get("model")
		if model.Type != gjson.String || model.Str == "" {
			return errBadBody
		}
		req.Model = model.Str
		req.Stream = root.Get("stream").Bool()
	}
	return nil
}

// populateRequestHeaders copies the client headers routing and billing need
// into req.ClientHeaders and req.PricingHeaders, stripping credentials from
// the latter.
func populateRequestHeaders(r *http.Request, req *Request) {
	if version := r.URL.Query().Get("api-version"); version != "" {
		req.ClientHeaders = map[string]string{"X-Spark-Api-Version": version}
	}
	for _, name := range []string{"Anthropic-Beta", "Anthropic-Version", "OpenAI-Beta", "X-Codex-Turn-State"} {
		if value := r.Header.Get(name); value != "" {
			if req.ClientHeaders == nil {
				req.ClientHeaders = make(map[string]string)
			}
			req.ClientHeaders[name] = value
		}
	}
	req.PricingHeaders = make(map[string]string, len(r.Header))
	for name, values := range r.Header {
		switch strings.ToLower(name) {
		case "authorization", "x-api-key", "x-goog-api-key", "cookie", "proxy-authorization":
			continue
		}
		req.PricingHeaders[name] = strings.Join(values, ",")
	}
}

// apiKeyFrom accepts "Authorization: Bearer <key>" and "x-api-key: <key>".
func apiKeyFrom(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if key, ok := strings.CutPrefix(auth, "Bearer "); ok {
			return strings.TrimSpace(key)
		}
		return ""
	}
	if key := strings.TrimSpace(r.Header.Get("X-Api-Key")); key != "" {
		return key
	}
	if key := strings.TrimSpace(r.Header.Get("X-Goog-Api-Key")); key != "" {
		return key
	}
	return strings.TrimSpace(r.URL.Query().Get("key"))
}

func newRequestID() string {
	var b [12]byte
	_, _ = rand.Read(b[:]) // crypto/rand never fails on supported platforms
	return "req_" + hex.EncodeToString(b[:])
}

// estimateUsage is the local fallback when the upstream reports no usage.
// Roughly four bytes per token; flagged so reports can tell it apart.
func estimateUsage(req *Request, deliveredTextBytes int64) Usage {
	return Usage{
		PromptTokens:     int64(len(req.Body)+3) / 4,
		CompletionTokens: (deliveredTextBytes + 3) / 4,
		Estimated:        true,
	}
}
