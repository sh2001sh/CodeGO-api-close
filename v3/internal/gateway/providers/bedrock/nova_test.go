package bedrock

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestNovaBuildTextImageAndTools(t *testing.T) {
	req := &gateway.Request{Protocol: gateway.ProtocolAnthropic, Model: "nova-pro-v1:0", Body: []byte(`{"model":"nova-pro-v1:0","max_tokens":128,"system":"system","messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]},{"role":"assistant","content":[{"type":"tool_use","id":"tool-1","name":"lookup","input":{"word":"hello"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"result","is_error":true}]}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"lookup"}}`)}
	out, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{Secret: "test|us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(out.Body)
	_ = out.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(body, "schemaVersion").Str != "messages-v1" || gjson.GetBytes(body, "inferenceConfig.maxTokens").Int() != 128 || gjson.GetBytes(body, "system.0.text").Str != "system" {
		t.Fatalf("bad Nova request %s", body)
	}
	if gjson.GetBytes(body, "messages.0.content.1.image.source.bytes").Str != "aGVsbG8=" || gjson.GetBytes(body, "messages.1.content.0.toolUse.toolUseId").Str != "tool-1" || gjson.GetBytes(body, "messages.2.content.0.toolResult.status").Str != "error" {
		t.Fatalf("content conversion lost fields: %s", body)
	}
	if gjson.GetBytes(body, "toolConfig.toolChoice.tool.name").Str != "lookup" || gjson.GetBytes(body, "anthropic_version").Exists() {
		t.Fatal("tool or native format corrupted")
	}
}

func TestNovaBinaryTextAndToolUsage(t *testing.T) {
	var fixture []byte
	for _, data := range []string{
		`{"messageStart":{"role":"assistant"}}`,
		`{"contentBlockDelta":{"contentBlockIndex":0,"delta":{"text":"hello"}}}`,
		`{"contentBlockStop":{"contentBlockIndex":0}}`,
		`{"contentBlockStart":{"contentBlockIndex":1,"start":{"toolUse":{"toolUseId":"t","name":"lookup"}}}}`,
		`{"contentBlockDelta":{"contentBlockIndex":1,"delta":{"toolUse":{"input":"{\"word\":\"hello\"}"}}}}`,
		`{"contentBlockStop":{"contentBlockIndex":1}}`,
		`{"messageStop":{"stopReason":"tool_use"}}`,
		`{"metadata":{"usage":{"inputTokens":10,"outputTokens":7}}}`,
	} {
		fixture = append(fixture, chunkFrame(data)...)
	}
	for _, protocol := range []gateway.Protocol{gateway.ProtocolOpenAIChat, gateway.ProtocolAnthropic} {
		stream := decodeBinary(protocol, fixture)
		textBytes := 0
		var usage *gateway.Usage
		var payload strings.Builder
		done := false
		for i := 0; i < 30; i++ {
			ev, err := stream.Next()
			if err != nil {
				t.Fatal(err)
			}
			textBytes += ev.TextBytes
			payload.Write(ev.Payload)
			if ev.Usage != nil {
				usage = ev.Usage
			}
			if ev.Kind == gateway.EventDone {
				done = true
				break
			}
		}
		_ = stream.Close()
		if !done || textBytes != 21 || usage == nil || usage.PromptTokens != 10 || usage.CompletionTokens != 7 || !strings.Contains(payload.String(), "lookup") {
			t.Fatalf("Nova stream lost text/tool/usage: done %v bytes %d usage %+v", done, textBytes, usage)
		}
	}
}

func TestNovaRejectsBrokenLifecycle(t *testing.T) {
	for _, events := range [][]string{
		{`{"contentBlockDelta":{"contentBlockIndex":0,"delta":{"text":"hello"}}}`},
		{`{"messageStart":{"role":"assistant"}}`, `{"contentBlockDelta":{"contentBlockIndex":0,"delta":{"toolUse":{"input":"{}"}}}}`},
		{`{"messageStart":{"role":"assistant"}}`, `{"metadata":{"usage":{"inputTokens":1}}}`},
		{`{"messageStart":{"role":"assistant"}}`, `{"contentBlockDelta":{"contentBlockIndex":0,"delta":{"text":"hello"}}}`, `{"messageStop":{"stopReason":"end_turn"}}`},
	} {
		var converter novaStream
		var err error
		for _, event := range events {
			_, err = converter.convert([]byte(event))
			if err != nil {
				break
			}
		}
		if err == nil {
			t.Fatalf("invalid Nova stream accepted: %v", events)
		}
	}
}
