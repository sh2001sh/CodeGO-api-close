package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesTerminalErrorDistinguishesFailureFromTokenLimit(t *testing.T) {
	for _, body := range []string{
		`{"type":"response.error","error":{"code":"overloaded","message":"retry later"}}`,
		`{"type":"response.failed","response":{"status":"failed"}}`,
		`{"type":"response.incomplete","response":{"error":{"message":"provider unavailable"}}}`,
		`{"type":"response.cancelled"}`,
		`{"error":{"message":"provider unavailable"}}`,
	} {
		var event ResponsesStreamResponse
		require.NoError(t, json.Unmarshal([]byte(body), &event))
		require.NotNil(t, event.TerminalError(), body)
		require.NotEmpty(t, event.TerminalError().Type)
	}
	for _, body := range []string{
		`{"type":"response.created"}`,
		`{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"output_tokens":9}}}`,
		`{"type":"response.completed","response":{"status":"completed","error":null}}`,
		`{"error":{}}`,
	} {
		var event ResponsesStreamResponse
		require.NoError(t, json.Unmarshal([]byte(body), &event))
		require.Nil(t, event.TerminalError(), body)
	}
}
