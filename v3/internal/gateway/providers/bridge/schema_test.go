package bridge

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
	"github.com/tidwall/gjson"
)

func TestResponsesStructuredOutputMapsSchemaWithoutNativeType(t *testing.T) {
	req := request(gateway.ProtocolResponses, false)
	req.Body = []byte(`{"input":"hello","text":{"format":{"type":"json_schema","name":"result","strict":true,"schema":{"type":"object","properties":{"answer":{"type":"string"}}}}}}`)
	chat := &fakeProvider{}
	if _, err := (Provider{Chat: chat}).BuildRequest(context.Background(), req, gateway.Target{}); err != nil {
		t.Fatal(err)
	}
	root := gjson.ParseBytes(chat.built.Body)
	if root.Get("response_format.type").Str != "json_schema" || root.Get("response_format.json_schema.name").Str != "result" || !root.Get("response_format.json_schema.strict").Bool() || root.Get("response_format.json_schema.schema.properties.answer.type").Str != "string" {
		t.Fatalf("schema conversion failed: %s", chat.built.Body)
	}
	if root.Get("response_format.json_schema.type").Exists() {
		t.Fatal("Responses format.type leaked into Chat json_schema")
	}
}

func TestGeminiSchemaUppercaseTypesAndNullableAreConvertedRecursively(t *testing.T) {
	req := request(gateway.ProtocolGemini, false)
	req.Body = []byte(`{"contents":[{"parts":[{"text":"hi"}]}],"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"OBJECT","properties":{"items":{"type":"ARRAY","items":{"type":"STRING","nullable":true}}},"required":["items"]}}]}]}`)
	chat := &fakeProvider{}
	if _, err := (Provider{Chat: chat}).BuildRequest(context.Background(), req, gateway.Target{}); err != nil {
		t.Fatal(err)
	}
	root := gjson.GetBytes(chat.built.Body, "tools.0.function.parameters")
	if root.Get("type").Str != "object" || root.Get("properties.items.type").Str != "array" || root.Get("properties.items.items.type.0").Str != "string" || root.Get("properties.items.items.type.1").Str != "null" || root.Get("required.0").Str != "items" || root.Get("properties.items.items.nullable").Exists() {
		t.Fatalf("invalid converted schema: %s", root.Raw)
	}
}

func TestGeminiParallelSameNameToolResultsKeepExplicitIDs(t *testing.T) {
	req := request(gateway.ProtocolGemini, false)
	req.Body = []byte(`{"contents":[{"role":"model","parts":[{"functionCall":{"name":"lookup","id":"a","args":{"x":1}}},{"functionCall":{"name":"lookup","id":"b","args":{"x":2}}}]},{"role":"user","parts":[{"functionResponse":{"name":"lookup","id":"b","response":{"result":2}}},{"functionResponse":{"name":"lookup","id":"a","response":{"result":1}}}]}]}`)
	chat := &fakeProvider{}
	if _, err := (Provider{Chat: chat}).BuildRequest(context.Background(), req, gateway.Target{}); err != nil {
		t.Fatal(err)
	}
	root := gjson.ParseBytes(chat.built.Body)
	if root.Get("messages.0.tool_calls.0.id").Str != "a" || root.Get("messages.0.tool_calls.1.id").Str != "b" || root.Get("messages.1.tool_call_id").Str != "b" || root.Get("messages.2.tool_call_id").Str != "a" {
		t.Fatalf("parallel tool IDs confused: %s", chat.built.Body)
	}
}

func TestRealChatAdapterBridgePreservesFinalUsageAndNativeMarkers(t *testing.T) {
	for _, protocol := range []gateway.Protocol{gateway.ProtocolResponses, gateway.ProtocolAnthropic, gateway.ProtocolGemini} {
		req := request(protocol, true)
		provider := Provider{Chat: openai.Provider{}}
		upstream, err := provider.BuildRequest(context.Background(), req, gateway.Target{BaseURL: "https://example.test", Secret: "secret", UpstreamModel: "mapped"})
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(upstream.Body)
		if err != nil {
			t.Fatal(err)
		}
		if gjson.GetBytes(body, "model").Str != "mapped" || !gjson.GetBytes(body, "stream_options.include_usage").Bool() {
			t.Fatalf("wrong upstream body: %s", body)
		}
		payload := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n"
		stream := provider.Decode(req, &http.Response{Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload))})
		var usage *gateway.Usage
		last := ""
		for {
			event, err := stream.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if event.Usage != nil {
				usage = event.Usage
			}
			if event.Kind == gateway.EventData {
				last = event.Name
				if string(event.Payload) == "[DONE]" {
					t.Fatal("Chat marker leaked into native stream")
				}
			}
		}
		if err := stream.Close(); err != nil {
			t.Fatal(err)
		}
		if usage == nil || usage.PromptTokens != 8 || usage.CompletionTokens != 2 {
			t.Fatal("real Chat adapter usage lost")
		}
		if protocol == gateway.ProtocolResponses && last != "response.completed" {
			t.Fatalf("last=%s", last)
		}
		if protocol == gateway.ProtocolAnthropic && last != "message_stop" {
			t.Fatalf("last=%s", last)
		}
	}
}
