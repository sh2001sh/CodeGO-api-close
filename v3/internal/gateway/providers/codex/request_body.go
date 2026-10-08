package codex

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/tidwall/gjson"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// Only top-level policy fields change. Large input, tools and extension values
// retain their exact JSON bytes instead of being decoded and re-encoded. The
// small field index lives only during preparation, never during the stream.
func codexRequestBody(data []byte, originalModel, upstreamModel string) ([]byte, error) {
	if !gjson.ValidBytes(data) || bytes.TrimSpace(data)[0] != '{' {
		return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "invalid_request",
			Message: "codex: body must be a JSON object"}
	}
	fields := make(map[string]gjson.Result)
	gjson.ParseBytes(data).ForEach(func(key, value gjson.Result) bool {
		fields[key.Str] = value // retain Decoder's last-key-wins behavior
		return true
	})
	fields["store"] = gjson.Parse("false")
	if instructions, exists := fields["instructions"]; !exists || instructions.Type == gjson.Null {
		fields["instructions"] = gjson.Parse(`""`)
	}
	if upstreamModel != "" && upstreamModel != originalModel {
		model, err := json.Marshal(upstreamModel)
		if err != nil {
			return nil, err
		}
		fields["model"] = gjson.Parse(string(model))
	}
	for _, field := range []string{"max_output_tokens", "max_tokens", "temperature", "top_p", "frequency_penalty", "presence_penalty"} {
		delete(fields, field)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	var result bytes.Buffer
	result.Grow(len(data) + 64)
	result.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			result.WriteByte(',')
		}
		name, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		result.Write(name)
		result.WriteByte(':')
		result.WriteString(fields[key].Raw)
	}
	result.WriteByte('}')
	return result.Bytes(), nil
}
