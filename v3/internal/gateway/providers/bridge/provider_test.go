package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type fakeProvider struct {
	built, decoded *gateway.Request
	source         gateway.EventStream
}

func (p *fakeProvider) BuildRequest(ctx context.Context, r *gateway.Request, _ gateway.Target) (*http.Request, error) {
	copy := *r
	p.built = &copy
	return http.NewRequestWithContext(ctx, "POST", "https://example.test", strings.NewReader(string(r.Body)))
}
func (p *fakeProvider) Decode(r *gateway.Request, _ *http.Response) gateway.EventStream {
	copy := *r
	p.decoded = &copy
	return p.source
}

type fakeStream struct {
	events []gateway.Event
	err    error
	closed bool
}

func (s *fakeStream) Next() (gateway.Event, error) {
	if len(s.events) > 0 {
		e := s.events[0]
		s.events = s.events[1:]
		return e, nil
	}
	if s.err != nil {
		return gateway.Event{}, s.err
	}
	return gateway.Event{}, io.EOF
}
func (s *fakeStream) Close() error { s.closed = true; return nil }
func request(protocol gateway.Protocol, stream bool) *gateway.Request {
	body := `{"model":"m","input":"hello"}`
	if protocol == gateway.ProtocolAnthropic {
		body = `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hello"}]}`
	}
	if protocol == gateway.ProtocolGemini {
		body = `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`
	}
	return &gateway.Request{ID: "test", Model: "m", Protocol: protocol, Stream: stream, Body: []byte(body)}
}
func decode(t *testing.T, req *gateway.Request, events []gateway.Event, err error) []gateway.Event {
	t.Helper()
	source := &fakeStream{events: events, err: err}
	p := Provider{Chat: &fakeProvider{source: source}}
	stream := p.Decode(req, &http.Response{Body: io.NopCloser(strings.NewReader(""))})
	defer func() {
		if err := stream.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()
	var out []gateway.Event
	for {
		event, e := stream.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatalf("decode: %v", e)
		}
		out = append(out, event)
	}
	return out
}
func chunk(delta string, usage *gateway.Usage) gateway.Event {
	return gateway.Event{Kind: gateway.EventData, Payload: []byte(`{"id":"chat1","model":"m","choices":[{"index":0,"delta":` + delta + `}]}`), Usage: usage}
}

func TestNativeAdapterHasPriorityAndDoesNotNormalize(t *testing.T) {
	req := request(gateway.ProtocolResponses, true)
	req.Body = []byte(`{"store":true,"input":"hi"}`)
	native := &fakeProvider{source: &fakeStream{}}
	p := Provider{Native: map[gateway.Protocol]gateway.Provider{gateway.ProtocolResponses: native}}
	if _, err := p.BuildRequest(context.Background(), req, gateway.Target{}); err != nil {
		t.Fatal(err)
	}
	if native.built.Protocol != req.Protocol || string(native.built.Body) != string(req.Body) {
		t.Fatal("native request changed")
	}
	if stream := p.Decode(req, &http.Response{}); stream != native.source {
		t.Fatal("native stream wrapped")
	}
}

func TestRequestConversionsPreserveMessagesImagesAndTools(t *testing.T) {
	cases := []struct {
		protocol  gateway.Protocol
		body      string
		image     string
		arguments string
	}{
		{gateway.ProtocolResponses, `{"model":"m","instructions":"system","max_output_tokens":21,"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"tool_choice":{"type":"function","name":"lookup"},"input":[{"role":"user","content":[{"type":"input_text","text":"see"},{"type":"input_image","image_url":"https://image.test/i","detail":"high"}]},{"type":"function_call","call_id":"c1","name":"lookup","arguments":"{\"x\":1}"},{"type":"function_call_output","call_id":"c1","output":"found"}]}`, "https://image.test/i", `{"x":1}`},
		{gateway.ProtocolAnthropic, `{"model":"m","system":"system","max_tokens":21,"tools":[{"name":"lookup","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"lookup"},"messages":[{"role":"user","content":[{"type":"text","text":"see"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAA"}}]},{"role":"assistant","content":[{"type":"tool_use","id":"c1","name":"lookup","input":{"x":1}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"found"}]}]}`, "data:image/png;base64,AAA", `{"x":1}`},
		{gateway.ProtocolGemini, `{"systemInstruction":{"parts":[{"text":"system"}]},"generationConfig":{"maxOutputTokens":21},"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"object"}}]}],"toolConfig":{"functionCallingConfig":{"mode":"ANY","allowedFunctionNames":["lookup"]}},"contents":[{"role":"user","parts":[{"text":"see"},{"fileData":{"mimeType":"image/png","fileUri":"https://image.test/i"}}]},{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"x":1}}}]},{"role":"user","parts":[{"functionResponse":{"name":"lookup","response":{"result":"found"}}}]}]}`, "https://image.test/i", `{"x":1}`},
	}
	for _, tc := range cases {
		t.Run(string(rune('0'+tc.protocol)), func(t *testing.T) {
			req := request(tc.protocol, true)
			req.Body = []byte(tc.body)
			chat := &fakeProvider{source: &fakeStream{}}
			p := Provider{Chat: chat}
			if _, err := p.BuildRequest(context.Background(), req, gateway.Target{}); err != nil {
				t.Fatal(err)
			}
			body := chat.built.Body
			for path, want := range map[string]string{"messages.0.role": "system", "messages.1.content.0.text": "see", "messages.1.content.1.image_url.url": tc.image, "messages.2.tool_calls.0.function.arguments": tc.arguments, "messages.3.role": "tool", "tools.0.function.name": "lookup", "tool_choice.function.name": "lookup"} {
				if got := gjson.GetBytes(body, path).Str; got != want {
					t.Fatalf("%s=%q want %q", path, got, want)
				}
			}
			if gjson.GetBytes(body, "max_tokens").Int() != 21 || !gjson.GetBytes(body, "stream_options.include_usage").Bool() {
				t.Fatal("token limit or usage option lost")
			}
			if req.Protocol != tc.protocol || string(req.Body) != tc.body {
				t.Fatal("original request mutated")
			}
			p.Decode(req, &http.Response{})
			if string(chat.decoded.Body) != string(chat.built.Body) {
				t.Fatal("build/decode normalization differs")
			}
		})
	}
}

func TestLossyNativeFeaturesAreRejectedBeforeUpstream(t *testing.T) {
	cases := []struct {
		protocol gateway.Protocol
		body     string
		field    string
	}{
		{gateway.ProtocolResponses, `{"model":"m","input":"hi","previous_response_id":"r1"}`, "previous_response_id"},
		{gateway.ProtocolAnthropic, `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}]}`, "cache_control"},
		{gateway.ProtocolGemini, `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":10}}}`, "thinkingConfig"},
	}
	for _, tc := range cases {
		req := request(tc.protocol, true)
		req.Body = []byte(tc.body)
		chat := &fakeProvider{}
		_, err := (Provider{Chat: chat}).BuildRequest(context.Background(), req, gateway.Target{})
		if err == nil || !strings.Contains(err.Error(), tc.field) {
			t.Fatalf("error=%v want field %s", err, tc.field)
		}
		var requestError *gateway.UpstreamError
		if !errors.As(err, &requestError) || requestError.Status != http.StatusBadRequest {
			t.Fatalf("conversion failure must be a typed caller error: %v", err)
		}
		if chat.built != nil {
			t.Fatal("upstream called after lossy request")
		}
	}
}

func TestNonStreamingNativeResponsesRetainToolsReasoningAndUsage(t *testing.T) {
	usage := &gateway.Usage{PromptTokens: 15, CompletionTokens: 4, CachedTokens: 5}
	payload := []byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"answer","reasoning_content":"think","tool_calls":[{"id":"c1","type":"function","function":{"name":"lookup","arguments":"{\"x\":1}"}}]},"finish_reason":"tool_calls"}]}`)
	for _, protocol := range []gateway.Protocol{gateway.ProtocolResponses, gateway.ProtocolAnthropic, gateway.ProtocolGemini} {
		out := decode(t, request(protocol, false), []gateway.Event{{Kind: gateway.EventData, Payload: payload, Usage: usage}}, nil)
		if len(out) != 1 || out[0].Kind != gateway.EventData || out[0].Usage != usage || !json.Valid(out[0].Payload) {
			t.Fatalf("bad nonstream response: %#v", out)
		}
		body := out[0].Payload
		var text, tool, reasoning, tokens string
		switch protocol {
		case gateway.ProtocolResponses:
			text = "output.1.content.0.text"
			tool = "output.2.name"
			reasoning = "output.0.summary.0.text"
			tokens = "usage.input_tokens"
		case gateway.ProtocolAnthropic:
			text = "content.1.text"
			tool = "content.2.name"
			reasoning = "content.0.thinking"
			tokens = "usage.input_tokens"
		case gateway.ProtocolGemini:
			text = "candidates.0.content.parts.1.text"
			tool = "candidates.0.content.parts.2.functionCall.name"
			reasoning = "candidates.0.content.parts.0.text"
			tokens = "usageMetadata.promptTokenCount"
		}
		if gjson.GetBytes(body, text).Str != "answer" || gjson.GetBytes(body, tool).Str != "lookup" || gjson.GetBytes(body, reasoning).Str != "think" {
			t.Fatalf("lost native data: %s", body)
		}
		want := int64(15)
		if protocol == gateway.ProtocolAnthropic {
			want = 10
		}
		if got := gjson.GetBytes(body, tokens).Int(); got != want {
			t.Fatalf("tokens=%d want %d", got, want)
		}
	}
}

func TestUpstreamErrorsAndCutsNeverFabricateCompletion(t *testing.T) {
	failure := &gateway.UpstreamError{Status: 502, Type: "upstream_error", Code: "failed", Message: "failed"}
	for _, protocol := range []gateway.Protocol{gateway.ProtocolResponses, gateway.ProtocolAnthropic, gateway.ProtocolGemini} {
		out := decode(t, request(protocol, true), []gateway.Event{chunk(`{"content":"partial"}`, nil), {Kind: gateway.EventError, Err: failure}}, nil)
		if out[len(out)-1].Err != failure {
			t.Fatal("upstream error replaced")
		}
		for _, ev := range out {
			if ev.Name == "response.completed" || ev.Name == "message_stop" || ev.Kind == gateway.EventDone {
				t.Fatal("completion fabricated after error")
			}
		}
		p := Provider{Chat: &fakeProvider{source: &fakeStream{events: []gateway.Event{chunk(`{"content":"partial"}`, nil)}, err: io.ErrUnexpectedEOF}}}
		stream := p.Decode(request(protocol, true), &http.Response{})
		for {
			_, err := stream.Next()
			if err != nil {
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatal(err)
				}
				break
			}
		}
	}
}
