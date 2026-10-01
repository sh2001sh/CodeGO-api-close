package tencent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestRequestPreservesNativeBodyCredentialsAndZeroSampling(t *testing.T) {
	req := fixtureRequest(`{"messages":[{"role":"system","content":"rules"},{"role":"user","content":"one"},{"role":"assistant","content":"two"},{"role":"user","content":[{"type":"text","text":"three"}]}],"temperature":0,"top_p":0.8}`, true)
	target := gateway.Target{BaseURL: "https://hunyuan.tencentcloudapi.com/", Secret: "Bearer 123|test-id|test-secret", UpstreamModel: "hunyuan-lite"}
	out, err := (Provider{}).BuildRequest(context.Background(), req, target)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatal(err)
	}
	root := gjson.ParseBytes(data)
	if out.URL.String() != "https://hunyuan.tencentcloudapi.com/" || out.Header.Get("X-TC-Action") != "ChatCompletions" || out.Header.Get("X-TC-Version") != "2023-09-01" || out.Header.Get("Accept") != "text/event-stream" {
		t.Fatalf("wrong Tencent headers: %s", out.URL)
	}
	if root.Get("Model").Str != "hunyuan-lite" || root.Get("Messages.1.Content").Str != "one" || root.Get("Messages.3.Content").Str != "three" || !root.Get("Temperature").Exists() || root.Get("Temperature").Float() != 0 || root.Get("TopP").Float() != 0.8 || !root.Get("Stream").Bool() || root.Get("model").Exists() {
		t.Fatalf("lost native body semantics: %s", data)
	}
	if !strings.HasPrefix(out.Header.Get("Authorization"), "TC3-HMAC-SHA256 Credential=test-id/") || strings.Contains(out.Header.Get("Authorization"), "test-secret") {
		t.Fatal("incorrect authorization or exposed signing secret")
	}
}

func TestUnsupportedOrMalformedRequestsFailBeforeNetwork(t *testing.T) {
	for _, body := range []string{
		`{`, `{}`, `{"messages":[]}`, `{"messages":null}`,
		`{"messages":[{"role":"tool","content":"x"}]}`,
		`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"x"}}]}]}`,
		`{"messages":[{"role":"user","content":null}]}`,
		`{"messages":[{"role":"user","content":"x"}],"tools":[{}]}`,
		`{"messages":[{"role":"user","content":"x"}],"max_tokens":12}`,
		`{"messages":[{"role":"user","content":"x"}],"n":2}`,
		`{"messages":[{"role":"user","content":"x"}],"top_p":"0.5"}`,
		`{"messages":[{"role":"user","content":"x"}],"temperature":2.1}`,
	} {
		_, err := (Provider{}).BuildRequest(context.Background(), fixtureRequest(body, false), gateway.Target{Secret: "123|test-id|test-secret"})
		var typed *gateway.UpstreamError
		if !errors.As(err, &typed) || typed.Status != http.StatusBadRequest {
			t.Fatalf("request accepted or wrong error for %s: %v", body, err)
		}
	}
}

func TestMalformedCredentialsNeverAppearInErrors(t *testing.T) {
	for _, secret := range []string{"sensitive-value", "invalid-app|sensitive-id|sensitive-value", "123||sensitive-value", "123|id\nAuthorization|sensitive-value"} {
		_, err := (Provider{}).BuildRequest(context.Background(), fixtureRequest(`{"messages":[{"role":"user","content":"x"}]}`, false), gateway.Target{Secret: secret})
		if err == nil || strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), "Authorization") {
			t.Fatalf("accepted malformed secret or exposed it: %v", err)
		}
	}
	for _, endpoint := range []string{"ftp://host", "https://name:sensitive-value@host", "https://host/?token=sensitive-value", "https://host/#fragment"} {
		_, err := (Provider{}).BuildRequest(context.Background(), fixtureRequest(`{"messages":[{"role":"user","content":"x"}]}`, false), gateway.Target{Secret: "123|test-id|test-secret", BaseURL: endpoint})
		if err == nil || strings.Contains(err.Error(), "sensitive") {
			t.Fatalf("accepted invalid endpoint or exposed it: %v", err)
		}
	}
}

func TestSingleResponseMapsNativeTextReasoningFinishAndUsage(t *testing.T) {
	s := fixtureStream(false, `{"Response":{"Id":"r1","Created":123,"Choices":[{"Message":{"Role":"assistant","Content":"hello","ReasoningContent":"reason"},"FinishReason":"length"}],"Usage":{"PromptTokens":9,"CompletionTokens":2,"TotalTokens":11}}}`, false)
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventData || ev.TextBytes != 11 || ev.Usage == nil || ev.Usage.PromptTokens != 9 || ev.Usage.CompletionTokens != 2 || gjson.GetBytes(ev.Payload, "choices.0.message.reasoning_content").Str != "reason" || gjson.GetBytes(ev.Payload, "choices.0.finish_reason").Str != "length" || gjson.GetBytes(ev.Payload, "model").Str != "alias" {
		t.Fatalf("wrong converted response: %+v %s %v", ev, ev.Payload, err)
	}
	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

func TestStreamUsageOnlyCollectedAndNativeEOFEndsCleanly(t *testing.T) {
	data := event(`{"Choices":[{"Delta":{"Role":"assistant","Content":""}}]}`) +
		event(`{"Id":"r1","Choices":[{"Delta":{"Content":"hello"}}]}`) +
		event(`{"Choices":[{"Delta":{"Content":""},"FinishReason":"stop"}]}`) +
		event(`{"Usage":{"PromptTokens":8,"CompletionTokens":0,"TotalTokens":8}}`)
	for _, want := range []bool{false, true} {
		s := fixtureStream(true, data, want)
		ev, err := s.Next()
		if err != nil || ev.Kind != gateway.EventData || ev.TextBytes != 5 || gjson.GetBytes(ev.Payload, "choices.0.delta.role").Str != "assistant" {
			t.Fatalf("empty lifecycle committed or text missing: %+v %v", ev, err)
		}
		ev, err = s.Next()
		if err != nil || gjson.GetBytes(ev.Payload, "choices.0.finish_reason").Str != "stop" {
			t.Fatalf("missing finish reason: %+v %v", ev, err)
		}
		ev, err = s.Next()
		expected := gateway.EventUsage
		if want {
			expected = gateway.EventData
		}
		if err != nil || ev.Kind != expected || ev.Usage == nil || ev.Usage.PromptTokens != 8 || ev.Usage.CompletionTokens != 0 {
			t.Fatalf("zero completion usage lost: %+v %v", ev, err)
		}
		if want && gjson.GetBytes(ev.Payload, "choices.#").Int() != 0 {
			t.Fatalf("usage-only chunk has choices: %s", ev.Payload)
		}
		ev, err = s.Next()
		if err != nil || ev.Kind != gateway.EventDone {
			t.Fatalf("native EOF not considered clean: %+v %v", ev, err)
		}
		if _, err = s.Next(); !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		_ = s.Close()
	}
}

func TestErrorsAndEmptyResponsesNeverBecomeSuccessfulChunks(t *testing.T) {
	for _, data := range []string{
		`{"Response":{"Error":{"Code":"AuthFailure.SignatureFailure","Message":"invalid signature"}}}`,
		`{"Error":{"Code":400,"Message":"bad request"}}`, `{}`,
		`{"Response":{"Choices":[]}}`, `{"Choices":[{"Message":{"Content":""},"FinishReason":"stop"}]}`,
	} {
		s := fixtureStream(false, data, false)
		ev, err := s.Next()
		if err != nil || ev.Kind != gateway.EventError || ev.Err == nil {
			t.Fatalf("error/empty body became success: %+v %v", ev, err)
		}
		_ = s.Close()
	}
	s := fixtureStream(true, event(`{"Choices":[{"Delta":{"Content":""},"FinishReason":"stop"}],"Usage":{"PromptTokens":0,"CompletionTokens":0}}`), false)
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventError || ev.Err.Code != "empty_response" || ev.Usage == nil {
		t.Fatalf("empty stream became visible: %+v %v", ev, err)
	}
}

func TestTruncatedAndMalformedStreamsFail(t *testing.T) {
	for _, data := range []string{"", "data: {bad}\n\n", "data: {}", event(`{"Choices":[{"Delta":{"Content":"hello"}}]}`), event(`{"Choices":[{"Delta":{"Content":"hello"}}]}`) + event(`[DONE]`)} {
		s := fixtureStream(true, data, false)
		var last error
		for i := 0; i < 5; i++ {
			ev, err := s.Next()
			if err != nil {
				last = err
				break
			}
			if ev.Kind == gateway.EventDone {
				t.Fatalf("truncated stream became clean: %q", data)
			}
		}
		_ = s.Close()
		if last == nil || errors.Is(last, io.EOF) {
			t.Fatalf("truncation/malformed data not detected: %q %v", data, last)
		}
	}
}

func TestMissingAndInvalidUsageCannotBecomeZeroTokens(t *testing.T) {
	for _, usage := range []string{"", `,"Usage":{"PromptTokens":4}`} {
		s := fixtureStream(false, `{"Choices":[{"Message":{"Content":"hello"},"FinishReason":"stop"}]`+usage+`}`, false)
		ev, err := s.Next()
		_ = s.Close()
		if err != nil || ev.Kind != gateway.EventData || ev.Usage != nil {
			t.Fatalf("missing usage became zero: %+v %v", ev, err)
		}
	}
	for _, tokens := range []string{`-1`, `1.5`, `"4"`, `9223372036854775808`, `9223372036854775807`} {
		s := fixtureStream(false, `{"Choices":[{"Message":{"Content":"hello"}}],"Usage":{"PromptTokens":`+tokens+`,"CompletionTokens":1}}`, false)
		_, err := s.Next()
		_ = s.Close()
		if err == nil {
			t.Fatalf("invalid usage accepted: %s", tokens)
		}
	}
}

func fixtureRequest(body string, stream bool) *gateway.Request {
	return &gateway.Request{ID: "fixture", Received: time.Unix(123, 0), Protocol: gateway.ProtocolOpenAIChat, Body: []byte(body), Model: "alias", Stream: stream}
}

func fixtureStream(stream bool, data string, wantUsage bool) gateway.EventStream {
	body, contentType := `{}`, "application/json"
	if wantUsage {
		body = `{"stream_options":{"include_usage":true}}`
	}
	if stream {
		contentType = "text/event-stream"
	}
	return (Provider{}).Decode(fixtureRequest(body, stream), &http.Response{Body: io.NopCloser(strings.NewReader(data)), Header: http.Header{"Content-Type": {contentType}}})
}

func event(data string) string { return "data: " + data + "\n\n" }
