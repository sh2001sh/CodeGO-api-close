package openai

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestFallbackCountsEveryChoiceAndGeneratedField(t *testing.T) {
	fields := `"content":"hello","reasoning":"think","reasoning_content":"trace","refusal":"no","tool_calls":[{"function":{"arguments":"{}"}}],"function_call":{"arguments":"[]"}`
	for _, streaming := range []bool{true, false} {
		field := "message"
		if streaming {
			field = "delta"
		}
		body := `{"choices":[{"index":0,"` + field + `":{` + fields + `}},{"index":1,"` + field + `":{"content":"world"}}]}`
		var source gateway.EventStream
		if streaming {
			source = decoder("data: " + body + "\n\ndata: [DONE]\n\n")
		} else {
			source = (Provider{}).Decode(&gateway.Request{}, &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))})
		}
		event, err := source.Next()
		_ = source.Close()
		if err != nil || event.TextBytes != 26 || event.Usage != nil {
			t.Fatalf("streaming=%v bytes=%d usage=%+v err=%v", streaming, event.TextBytes, event.Usage, err)
		}
	}
}

func TestEmptyNonStreamingDoesNotCommitData(t *testing.T) {
	for _, body := range []string{
		`{"choices":[]}`,
		`{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":0}}`,
	} {
		source := (Provider{}).Decode(&gateway.Request{}, &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))})
		event, err := source.Next()
		_ = source.Close()
		if err != nil || event.Kind == gateway.EventData {
			t.Fatalf("empty response committed client output: %+v %v", event, err)
		}
	}
}

func TestErrorEventPreservesLatestUsage(t *testing.T) {
	source := decoder("data: " + `{"error":{"code":"failed","message":"busy"},"usage":{"prompt_tokens":12,"completion_tokens":3}}` + "\n\n")
	event, err := source.Next()
	_ = source.Close()
	if err != nil || event.Kind != gateway.EventError || event.Usage == nil || event.Usage.CompletionTokens != 3 {
		t.Fatalf("failure usage lost: %+v %v", event, err)
	}
}
