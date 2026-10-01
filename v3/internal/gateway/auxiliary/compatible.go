package auxiliary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type compatibleAdapter struct{ kind string }

func (a compatibleAdapter) Build(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	if err := a.checkOperationSupported(in.Operation); err != nil {
		return nil, err
	}
	if err := validateCompatible(req, in); err != nil {
		return nil, err
	}
	address, err := compatibleURL(req, target, in.Operation, a.kind)
	if err != nil {
		return nil, err
	}
	body, contentType, err := a.buildBody(req, target, in)
	if err != nil {
		return nil, err
	}
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Content-Type", contentType)
	if err := a.applyAuth(out, target.Secret); err != nil {
		return nil, err
	}
	for name, value := range req.ClientHeaders {
		out.Header.Set(name, value)
	}
	if req.Stream {
		out.Header.Set("Accept", "text/event-stream")
	}
	return out, nil
}

// checkOperationSupported rejects operations the provider kind does not
// implement, including the kind-specific carve-outs for codex and azure.
func (a compatibleAdapter) checkOperationSupported(op Operation) error {
	if strings.HasPrefix(string(op), "gemini/") {
		return unsupported(op)
	}
	if !compatibleSupports(a.kind, op) {
		return unsupported(op)
	}
	if a.kind == "codex" && op != Compact && op != Search {
		return unsupported(op)
	}
	if a.kind == "azure" && op == Search {
		return unsupported(op)
	}
	return nil
}

// buildBody maps the client body into the upstream wire format and applies
// kind-specific body transforms (xAI image requests, portable web search).
func (a compatibleAdapter) buildBody(req *gateway.Request, target gateway.Target, in Input) ([]byte, string, error) {
	body, contentType, err := mappedBody(req, target, in)
	if err != nil {
		return nil, "", failure(400, "invalid_body", "could not convert request")
	}
	if a.kind == "xai" && (in.Operation == Images || in.Operation == ImageEdits) {
		body, err = xaiImageBody(body)
		if err != nil {
			return nil, "", err
		}
	}
	if in.Operation == Search && a.kind != "codex" {
		body, err = portableSearch(body, upstreamModel(req, target))
		if err != nil {
			return nil, "", err
		}
	}
	return body, contentType, nil
}

// applyAuth sets the outbound authorization headers for the provider kind,
// decoding the Codex access-token/account-id credential pair when needed.
func (a compatibleAdapter) applyAuth(out *http.Request, secret string) error {
	out.Header.Set("Authorization", "Bearer "+secret)
	switch a.kind {
	case "azure":
		out.Header.Del("Authorization")
		out.Header.Set("Api-Key", secret)
	case "codex":
		var key struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		}
		if json.Unmarshal([]byte(secret), &key) != nil || strings.TrimSpace(key.AccessToken) == "" || strings.TrimSpace(key.AccountID) == "" {
			return errors.New("auxiliary: invalid Codex credential")
		}
		out.Header.Set("Authorization", "Bearer "+key.AccessToken)
		out.Header.Set("Chatgpt-Account-Id", key.AccountID)
		out.Header.Set("OpenAI-Beta", "responses=experimental")
		out.Header.Set("Originator", "codex_cli_rs")
	}
	return nil
}

func compatibleURL(req *gateway.Request, target gateway.Target, op Operation, kind string) (string, error) {
	u, err := url.Parse(target.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("auxiliary: invalid channel URL")
	}
	base := strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/v1")
	path := string(op)
	if op == Search && kind != "codex" {
		path = "responses"
	}
	switch kind {
	case "deepseek":
		path = strings.TrimSuffix(base, "/beta") + "/beta/completions"
	case "azure":
		base = strings.TrimSuffix(base, "/openai")
		version := "2024-10-21"
		if op == Compact {
			path = base + "/openai/v1/responses/compact"
			version = "preview"
			if strings.HasSuffix(u.Hostname(), ".cognitiveservices.azure.com") {
				path = base + "/openai/responses/compact"
				version = "2024-10-21"
			}
		} else {
			path = base + "/openai/deployments/" + url.PathEscape(upstreamModel(req, target)) + "/" + path
		}
		query := u.Query()
		if query.Get("api-version") == "" {
			query.Set("api-version", version)
		}
		u.RawQuery = query.Encode()
	case "codex":
		base = strings.TrimSuffix(base, "/backend-api/codex")
		base = strings.TrimSuffix(base, "/backend-api")
		path = base + "/backend-api/codex/" + path
	default:
		if kind == "custom" && strings.Contains(target.BaseURL, "{model}") {
			return strings.ReplaceAll(target.BaseURL, "{model}", url.PathEscape(upstreamModel(req, target))), nil
		}
		path = base + "/v1/" + path
	}
	u.Path, u.RawPath, u.Fragment = path, "", ""
	return u.String(), nil
}

func mappedBody(req *gateway.Request, target gateway.Target, in Input) ([]byte, string, error) {
	media, params, _ := mime.ParseMediaType(in.ContentType)
	model := upstreamModel(req, target)
	if media != "multipart/form-data" {
		body, err := object(in.Body)
		if err != nil {
			return nil, "", err
		}
		body["model"], _ = json.Marshal(model)
		data, err := json.Marshal(body)
		return data, "application/json", err
	}
	reader := multipart.NewReader(bytes.NewReader(in.Body), params["boundary"])
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	seen := false
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", err
		}
		out, err := writer.CreatePart(part.Header)
		if err != nil {
			return nil, "", err
		}
		if part.FormName() == "model" {
			_, err = io.WriteString(out, model)
			seen = true
		} else {
			_, err = io.Copy(out, part)
		}
		if err != nil {
			return nil, "", err
		}
	}
	if !seen {
		if err := writer.WriteField("model", model); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

func (a compatibleAdapter) Decode(_ context.Context, req *gateway.Request, _ gateway.Target, in Input, resp *http.Response) (Response, error) {
	out, err := rawResponse(resp)
	if err != nil {
		return out, err
	}
	if in.Operation != Speech && in.Operation != Transcriptions && in.Operation != Translations && (!gjson.ValidBytes(out.Body) || !gjson.ParseBytes(out.Body).IsObject()) {
		return out, failure(502, "invalid_response", "upstream response must be a JSON object")
	}
	if value := gjson.GetBytes(out.Body, "error"); value.Exists() && value.Raw != "null" {
		return out, failure(502, "upstream_error", "upstream returned an error")
	}
	if err := validateCompatibleResponse(in.Operation, out.Body); err != nil {
		return out, err
	}
	if in.Operation == Compact {
		found := false
		for _, item := range gjson.GetBytes(out.Body, "output").Array() {
			typ := item.Get("type").String()
			found = found || typ == "compaction" || typ == "compaction_summary"
		}
		if !found {
			return out, failure(502, "invalid_compaction", "upstream returned no compaction output")
		}
	}
	if in.Operation == Search {
		if a.kind != "codex" {
			var texts []string
			for _, item := range gjson.GetBytes(out.Body, "output").Array() {
				for _, content := range item.Get("content").Array() {
					if content.Get("type").String() == "output_text" {
						texts = append(texts, content.Get("text").String())
					}
				}
			}
			if len(texts) == 0 {
				return out, failure(502, "empty_search", "upstream returned no search output")
			}
			out.Body, _ = json.Marshal(map[string]any{"output": strings.Join(texts, "\n")})
		}
		if out.Usage == nil {
			out.Usage = &gateway.Usage{Estimated: true}
		}
		if out.Usage.ToolCalls == nil {
			out.Usage.ToolCalls = make(map[string]int64)
		}
		out.Usage.ToolCalls["web_search"] = max(out.Usage.ToolCalls["web_search"], 1)
	}
	if out.Usage == nil {
		usage := estimate(req, in, out.Body)
		out.Usage = &usage
	}
	return out, nil
}

func validateCompatible(req *gateway.Request, in Input) error {
	body := gjson.ParseBytes(req.Body)
	valid := true
	switch in.Operation {
	case Embeddings, Moderations:
		valid = body.Get("input").Exists()
	case Images:
		valid = strings.TrimSpace(body.Get("prompt").String()) != ""
	case Edits:
		valid = strings.TrimSpace(body.Get("instruction").String()) != ""
	case ImageEdits:
		valid = body.Get("image").Exists() || body.Get("images").Exists() || body.Get("image_bytes").Int() > 0 || body.Get("image[]_bytes").Int() > 0
	case Transcriptions, Translations:
		valid = body.Get("file_bytes").Int() > 0
	case Speech:
		valid = strings.TrimSpace(body.Get("input").String()) != ""
	case Rerank:
		valid = strings.TrimSpace(body.Get("query").String()) != "" && len(body.Get("documents").Array()) > 0
	case Completions:
		valid = body.Get("prompt").Exists()
	case Compact:
		valid = body.Get("input").Exists()
	}
	if !valid {
		return failure(400, "invalid_request", "required endpoint input is missing")
	}
	return nil
}

func portableSearch(data []byte, model string) ([]byte, error) {
	request, err := object(data)
	if err != nil {
		return nil, err
	}
	tool := map[string]any{"type": "web_search"}
	var settings map[string]json.RawMessage
	if json.Unmarshal(request["settings"], &settings) == nil {
		for _, name := range []string{"search_context_size", "user_location", "filters"} {
			if value := settings[name]; len(value) > 0 {
				tool[name] = value
			}
		}
	}
	body := map[string]any{"model": model, "input": "Execute this standalone web search request using the web search tool. Follow every command in order and return useful results with source URLs. Request JSON:\n" + string(data), "tools": []any{tool}, "stream": false, "store": false}
	for _, name := range []string{"reasoning", "max_output_tokens"} {
		if value := request[name]; len(value) > 0 {
			body[name] = value
		}
	}
	return json.Marshal(body)
}
