package gateway

import (
	"strings"

	"github.com/tidwall/gjson"
)

// Dispatch only from the actual server path. Client pricing headers cannot
// select another prompt shape and bypass the caller's normal protocol policy.
func sensitiveAuxiliaryPrompt(req *Request) (string, bool) {
	root := gjson.ParseBytes(req.Body)
	var texts []string
	add := func(value gjson.Result) {
		if value.Type == gjson.String {
			texts = append(texts, value.String())
		}
	}
	addStrings := func(value gjson.Result) {
		if value.Type == gjson.String {
			add(value)
		} else if value.IsArray() {
			for _, item := range value.Array() {
				add(item)
			}
		}
	}

	path := req.Path
	ok := true
	switch {
	case path == "/v1/embeddings" || (strings.HasPrefix(path, "/v1/engines/") && strings.HasSuffix(path, "/embeddings")):
		addStrings(root.Get("input"))
	case path == "/v1/audio/speech":
		add(root.Get("input"))
	case path == "/v1/images/generations" || path == "/v1/images/edits":
		add(root.Get("prompt"))
	case path == "/v1/completions":
		addStrings(root.Get("prompt"))
	case path == "/v1/edits":
		addStrings(root.Get("input"))
		add(root.Get("instruction"))
	case path == "/v1/moderations":
		addStrings(root.Get("input"))
	case path == "/v1/rerank":
		add(root.Get("query"))
		for _, document := range root.Get("documents").Array() {
			if document.Type == gjson.String {
				add(document)
			} else {
				add(document.Get("text"))
			}
		}
	case strings.HasPrefix(path, "/v1/models/") || strings.HasPrefix(path, "/v1beta/models/"):
		ok = addGeminiModelActionText(path, root, add)
	default:
		ok = false
	}
	if !ok {
		return "", false
	}
	return strings.Join(texts, "\n"), true
}

// addGeminiModelActionText dispatches on the Gemini-style ":action" suffix of
// a /v1/models/{model}:action or /v1beta/models/{model}:action path, feeding
// matched text fields to add. It reports whether the action is recognized.
func addGeminiModelActionText(path string, root gjson.Result, add func(gjson.Result)) bool {
	addParts := func(content gjson.Result) {
		for _, part := range content.Get("parts").Array() {
			add(part.Get("text"))
		}
	}
	switch {
	case strings.HasSuffix(path, ":embedContent"):
		addParts(root.Get("content"))
	case strings.HasSuffix(path, ":batchEmbedContents"):
		for _, request := range root.Get("requests").Array() {
			addParts(request.Get("content"))
		}
	case strings.HasSuffix(path, ":predict"):
		for _, instance := range root.Get("instances").Array() {
			add(instance.Get("prompt"))
		}
	default:
		return false
	}
	return true
}
