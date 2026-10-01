package dify

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type nativeFile struct {
	Type         string `json:"type"`
	TransferMode string `json:"transfer_mode"`
	URL          string `json:"url,omitempty"`
	UploadFileID string `json:"upload_file_id,omitempty"`
}

type nativeRequest struct {
	Inputs           map[string]json.RawMessage `json:"inputs"`
	Query            string                     `json:"query"`
	ResponseMode     string                     `json:"response_mode"`
	User             string                     `json:"user"`
	ConversationID   string                     `json:"conversation_id,omitempty"`
	AutoGenerateName bool                       `json:"auto_generate_name"`
	Files            []nativeFile               `json:"files"`
}

const maxInlineImage = 10 << 20
const maxInlineTotal = 20 << 20

type inlineImage struct {
	index int
	mime  string
	data  []byte
}

func parseInlineImage(dataURL string, index int) (inlineImage, error) {
	prefix, encoded, ok := strings.Cut(dataURL, ",")
	if !ok || !strings.HasPrefix(prefix, "data:image/") || !strings.HasSuffix(prefix, ";base64") {
		return inlineImage{}, fmt.Errorf("dify: inline images require a base64 image data URL")
	}
	mime := strings.TrimSuffix(strings.TrimPrefix(prefix, "data:"), ";base64")
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return inlineImage{}, fmt.Errorf("dify: unsupported inline image MIME type")
	}
	if len(encoded) > base64.StdEncoding.EncodedLen(maxInlineImage) {
		return inlineImage{}, fmt.Errorf("dify: inline image exceeds 10 MiB")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > maxInlineImage {
		return inlineImage{}, fmt.Errorf("dify: invalid or oversized base64 image")
	}
	if http.DetectContentType(data) != mime {
		return inlineImage{}, fmt.Errorf("dify: inline image does not match its declared MIME type")
	}
	return inlineImage{index: index, mime: mime, data: data}, nil
}

func convertRequest(req *gateway.Request) ([]byte, []inlineImage, error) {
	root := gjson.ParseBytes(req.Body)
	if !gjson.ValidBytes(req.Body) || !root.IsObject() {
		return nil, nil, fmt.Errorf("dify: request must be a JSON object")
	}
	if invalid := validateChatRequestFields(root); invalid != "" {
		return nil, nil, fmt.Errorf("dify: unsupported Chat field %q; generation settings are configured in the Dify application", invalid)
	}
	out := nativeRequest{Inputs: map[string]json.RawMessage{}, Files: []nativeFile{}, ResponseMode: "blocking", User: req.ID}
	if req.Stream {
		out.ResponseMode = "streaming"
	}
	if err := applyScalarFields(root, req, &out); err != nil {
		return nil, nil, err
	}
	messages := root.Get("messages")
	if !messages.IsArray() || len(messages.Array()) == 0 {
		return nil, nil, fmt.Errorf("dify: at least one message is required")
	}
	query, images, err := convertMessages(messages, &out)
	if err != nil {
		return nil, nil, err
	}
	out.Query = query
	if strings.TrimSpace(out.Query) == "" {
		return nil, nil, fmt.Errorf("dify: query must contain text")
	}
	body, err := json.Marshal(out)
	return body, images, err
}

// validateChatRequestFields returns the name of the first top-level Chat
// field that cannot be converted losslessly to a Dify request, or "" if
// every field is acceptable.
func validateChatRequestFields(root gjson.Result) string {
	var invalid string
	root.ForEach(func(key, value gjson.Result) bool {
		if value.Type == gjson.Null {
			return true
		}
		switch key.Str {
		case "model", "messages", "stream", "user", "inputs", "conversation_id", "auto_generate_name":
		case "stream_options":
			if !value.IsObject() {
				invalid = key.Str
				break
			}
			value.ForEach(func(k, v gjson.Result) bool {
				if k.Str != "include_usage" || v.Type != gjson.True && v.Type != gjson.False {
					invalid = "stream_options." + k.Str
				}
				return invalid == ""
			})
		case "n":
			if value.Raw != "1" {
				invalid = key.Str
			}
		case "logprobs":
			if value.Type != gjson.False {
				invalid = key.Str
			}
		case "tool_choice":
			if value.Type != gjson.String || value.Str != "none" {
				invalid = key.Str
			}
		case "tools", "functions":
			if !value.IsArray() || len(value.Array()) != 0 {
				invalid = key.Str
			}
		default:
			invalid = key.Str
		}
		return invalid == ""
	})
	return invalid
}

// applyScalarFields copies inputs/user/conversation_id/auto_generate_name
// from the Chat request onto the native Dify request.
func applyScalarFields(root gjson.Result, req *gateway.Request, out *nativeRequest) error {
	if value := root.Get("inputs"); value.Exists() {
		if !value.IsObject() {
			return fmt.Errorf("dify: inputs must be an object")
		}
		if err := json.Unmarshal([]byte(value.Raw), &out.Inputs); err != nil {
			return err
		}
	}
	for key, dest := range map[string]*string{"user": &out.User, "conversation_id": &out.ConversationID} {
		if value := root.Get(key); value.Exists() {
			if value.Type != gjson.String {
				return fmt.Errorf("dify: %s must be a string", key)
			}
			*dest = value.Str
		}
	}
	if out.User == "" {
		out.User = req.ID
	}
	if out.User == "" {
		return fmt.Errorf("dify: user or request ID is required")
	}
	if value := root.Get("auto_generate_name"); value.Exists() {
		if value.Type != gjson.True && value.Type != gjson.False {
			return fmt.Errorf("dify: auto_generate_name must be a boolean")
		}
		out.AutoGenerateName = value.Bool()
	}
	return nil
}

// convertMessages renders Chat messages into Dify's single query string,
// collecting any inline image bytes that must be uploaded separately.
func convertMessages(messages gjson.Result, out *nativeRequest) (string, []inlineImage, error) {
	var query strings.Builder
	var images []inlineImage
	totalImageBytes := 0
	for _, msg := range messages.Array() {
		if !msg.IsObject() {
			return "", nil, fmt.Errorf("dify: messages must be objects")
		}
		var invalid string
		msg.ForEach(func(key, value gjson.Result) bool {
			if value.Type != gjson.Null && key.Str != "role" && key.Str != "content" {
				invalid = "messages." + key.Str
			}
			return invalid == ""
		})
		if invalid != "" {
			return "", nil, fmt.Errorf("dify: unsupported field %q", invalid)
		}
		role := msg.Get("role").Str
		label, err := difyRoleLabel(role)
		if err != nil {
			return "", nil, err
		}
		content := msg.Get("content")
		if content.Type == gjson.String {
			query.WriteString(label + ": \n" + content.Str + "\n")
			continue
		}
		if !content.IsArray() {
			return "", nil, fmt.Errorf("dify: message content must be text or an array")
		}
		for _, part := range content.Array() {
			if err := convertContentPart(part, role, label, out, &query, &images, &totalImageBytes); err != nil {
				return "", nil, err
			}
		}
	}
	return query.String(), images, nil
}

func difyRoleLabel(role string) (string, error) {
	switch role {
	case "system", "developer":
		return "SYSTEM", nil
	case "assistant":
		return "ASSISTANT", nil
	case "user":
		return "USER", nil
	default:
		return "", fmt.Errorf("dify: unsupported message role %q", role)
	}
}

// convertContentPart handles one message content part (text or image_url),
// appending to query for text or out.Files/images for images.
func convertContentPart(part gjson.Result, role, label string, out *nativeRequest, query *strings.Builder, images *[]inlineImage, totalImageBytes *int) error {
	if !part.IsObject() {
		return fmt.Errorf("dify: content parts must be objects")
	}
	switch part.Get("type").Str {
	case "text":
		if part.Get("text").Type != gjson.String {
			return fmt.Errorf("dify: text content must be a string")
		}
		query.WriteString(label + ": \n" + part.Get("text").Str + "\n")
		return nil
	case "image_url":
		if role != "user" {
			return fmt.Errorf("dify: images are supported only in user messages")
		}
		image := part.Get("image_url")
		imageURL := image.Get("url")
		if detail := image.Get("detail"); detail.Exists() && (detail.Type != gjson.String || detail.Str != "auto") {
			return fmt.Errorf("dify: explicit image detail is unsupported")
		}
		if strings.HasPrefix(imageURL.Str, "data:") {
			inline, err := parseInlineImage(imageURL.Str, len(out.Files))
			if err != nil {
				return err
			}
			*totalImageBytes += len(inline.data)
			if *totalImageBytes > maxInlineTotal {
				return fmt.Errorf("dify: inline images exceed 20 MiB in total")
			}
			*images = append(*images, inline)
			out.Files = append(out.Files, nativeFile{Type: "image", TransferMode: "local_file"})
			return nil
		}
		u, err := url.Parse(imageURL.Str)
		if imageURL.Type != gjson.String || err != nil || u.Host == "" || u.User != nil || u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("dify: images require an HTTP(S) URL or a base64 image data URL")
		}
		out.Files = append(out.Files, nativeFile{Type: "image", TransferMode: "remote_url", URL: imageURL.Str})
		return nil
	default:
		return fmt.Errorf("dify: unsupported content type %q", part.Get("type").Str)
	}
}
