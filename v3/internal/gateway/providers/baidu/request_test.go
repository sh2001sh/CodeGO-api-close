package baidu

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestBuildNativeRequestPreservesParametersAndEscapesToken(t *testing.T) {
	request := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "alias", Stream: true,
		Body: []byte(`{"messages":[{"role":"system","content":"rules"},{"role":"user","content":[{"type":"text","text":"hello"}]}],"temperature":0,"top_p":0.8,"frequency_penalty":0.2,"max_tokens":1,"user":"u"}`)}
	out, err := (Provider{}).BuildRequest(context.Background(), request, gateway.Target{BaseURL: "https://fixture.invalid/", Secret: "a+b&c", UpstreamModel: "ERNIE-4.0"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Method != http.MethodPost || out.URL.Path != "/rpc/2.0/ai_custom/v1/wenxinworkshop/chat/completions_pro" || out.URL.Query().Get("access_token") != "a+b&c" {
		t.Fatalf("native URL/token mismatch: %s %s", out.Method, out.URL)
	}
	if out.Header.Get("Authorization") != "" || out.Header.Get("Accept") != "text/event-stream" || out.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("wrong native headers: %v", out.Header)
	}
	body, _ := io.ReadAll(out.Body)
	root := gjson.ParseBytes(body)
	if root.Get("model").Exists() || root.Get("system").Str != "rules" || root.Get("messages.0.role").Str != "user" || root.Get("messages.0.content").Str != "hello" || !root.Get("stream").Bool() || root.Get("max_output_tokens").Int() != 2 || root.Get("temperature").Float() != 0 || !root.Get("temperature").Exists() || root.Get("penalty_score").Float() != 0.2 || root.Get("user_id").Str != "u" {
		t.Fatalf("native body mismatch: %s", body)
	}
}

func TestAllLegacyModelPaths(t *testing.T) {
	for model, expected := range map[string]string{
		"ERNIE-4.0": "completions_pro", "ERNIE-Bot-4": "completions_pro", "ERNIE-4.0-8K": "completions_pro",
		"ERNIE-Bot": "completions", "ERNIE-3.5-8K": "completions",
		"ERNIE-Bot-turbo": "eb-instant", "ERNIE-Lite-8K-0922": "eb-instant",
		"ERNIE-Speed": "ernie_speed", "ERNIE-Speed-8K": "ernie_speed", "ERNIE-Bot-8K": "ernie_bot_8k",
		"BLOOMZ-7B": "bloomz_7b1", "CUSTOM-MODEL": "custom-model",
	} {
		t.Run(model, func(t *testing.T) {
			out, err := (Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: model, Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, gateway.Target{Secret: "token"})
			if err != nil || !strings.HasSuffix(out.URL.Path, "/"+expected) {
				t.Fatalf("wrong path: %v %v", out, err)
			}
		})
	}
}

func TestUnsupportedOrMalformedInputFailsBeforeOAuth(t *testing.T) {
	for _, body := range []string{
		`{`, `[]`, `{"messages":[]}`, `{"messages":[{"role":"system","content":"rules"}]}`,
		`{"messages":[{"role":"tool","content":"hi"}]}`,
		`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"x"}}]}]}`,
		`{"messages":[{"role":"assistant","content":"hi","tool_calls":[{}]}]}`,
		`{"messages":[{"role":"user","content":"hi"}],"tools":[{}]}`,
		`{"messages":[{"role":"user","content":"hi"}],"n":2}`,
		`{"messages":[{"role":"user","content":"hi"}],"max_tokens":0}`,
		`{"messages":[{"role":"user","content":"hi"}],"temperature":"warm"}`,
	} {
		t.Run(body, func(t *testing.T) {
			_, err := (Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "m", Body: []byte(body)}, gateway.Target{Secret: "would|require-oauth"})
			failure, ok := err.(*gateway.UpstreamError)
			if !ok || failure.Status != http.StatusBadRequest {
				t.Fatalf("want request error before networking, got %v", err)
			}
		})
	}
}
