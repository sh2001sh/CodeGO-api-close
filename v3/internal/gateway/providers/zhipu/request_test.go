package zhipu

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestCredentialUsesNativeJWTMillisecondsAndHMAC(t *testing.T) {
	now := time.Unix(1700000000, 123000000)
	token, err := signCredential("test-id.test-secret", now)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jwt.Parse(token, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			t.Fatalf("method = %v", token.Method)
		}
		return []byte("test-secret"), nil
	}, jwt.WithoutClaimsValidation(), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !parsed.Valid || parsed.Header["sign_type"] != "SIGN" {
		t.Fatalf("signed token = %v %v", parsed, err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["api_key"] != "test-id" || int64(claims["timestamp"].(float64)) != now.UnixMilli() || int64(claims["exp"].(float64)) != now.Add(24*time.Hour).UnixMilli() {
		t.Fatalf("claims = %v", claims)
	}
	if _, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return []byte("wrong"), nil }, jwt.WithoutClaimsValidation()); err == nil {
		t.Fatal("wrong signing key accepted")
	}
	for _, key := range []string{"", "id", "id.", ".secret", "id.secret.more"} {
		_, err := signCredential(key, now)
		var failure *gateway.UpstreamError
		if !errors.As(err, &failure) || failure.Code != "invalid_credential" || strings.Contains(failure.Message, key) && key != "" && key != "id" {
			t.Fatalf("credential error = %v", err)
		}
	}
}

func TestRequestUsesNativePathPromptSamplingAndIncrementalStream(t *testing.T) {
	body := []byte(`{"model":"client","stream":true,"temperature":0,"top_p":1,"request_id":"r1","messages":[{"role":"developer","content":"Be brief"},{"role":"user","content":[{"type":"text","text":"hello"},{"type":"text","text":" world"}]}],"stream_options":{"include_usage":true}}`)
	for _, stream := range []bool{true, false} {
		for _, base := range []string{"https://example.com", "https://example.com/", "https://example.com/api/paas/v3/"} {
			request, err := (Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "client", Body: body, Stream: stream}, gateway.Target{BaseURL: base, Secret: "test-id.test-secret", UpstreamModel: "chatglm_pro"})
			if err != nil {
				t.Fatal(err)
			}
			method := "invoke"
			if stream {
				method = "sse-invoke"
			}
			data, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			if request.URL.String() != "https://example.com/api/paas/v3/model-api/chatglm_pro/"+method || request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" || strings.HasPrefix(request.Header.Get("Authorization"), "Bearer ") || request.Header.Get("Authorization") == "" {
				t.Fatalf("request = %s %v", request.URL, request.Header)
			}
			if (request.Header.Get("Accept") == "text/event-stream") != stream || gjson.GetBytes(data, "incremental").Bool() != stream {
				t.Fatalf("stream request = %s %v", data, request.Header)
			}
			for path, want := range map[string]string{"prompt.0.role": "system", "prompt.1.content": "Okay", "prompt.2.content": "hello world", "temperature": "0", "top_p": "0.99", "request_id": "r1"} {
				if got := gjson.GetBytes(data, path).String(); got != want {
					t.Fatalf("%s = %q want %q; body = %s", path, got, want, data)
				}
			}
			if gjson.GetBytes(data, "messages").Exists() || gjson.GetBytes(data, "stream_options").Exists() {
				t.Fatalf("non-native fields = %s", data)
			}
		}
	}
	if gjson.GetBytes(body, "top_p").Int() != 1 || gjson.GetBytes(body, "model").Str != "client" {
		t.Fatal("original request mutated")
	}
}

func TestUnsupportedRequestsRejectWithoutDroppingInput(t *testing.T) {
	for _, body := range []string{
		`not JSON`, `[]`, `{"messages":[]}`, `{"messages":[null]}`,
		`{"messages":[{"role":"user","content":null}]}`,
		`{"messages":[{"role":"tool","content":"result"}]}`,
		`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/img"}}]}]}`,
		`{"messages":[{"role":"assistant","content":"hi","tool_calls":[{}]}]}`,
		`{"messages":[{"role":"user","content":"hi","name":"Bob"}]}`,
		`{"messages":[{"role":"user","content":[{"type":"text","text":"hi","extra":"lost"}]}]}`,
		`{"messages":[{"role":"user","content":"hi"}],"tools":[{}]}`,
		`{"messages":[{"role":"user","content":"hi"}],"max_tokens":10}`,
		`{"messages":[{"role":"user","content":"hi"}],"n":2}`,
		`{"messages":[{"role":"user","content":"hi"}],"n":1.2}`,
		`{"messages":[{"role":"user","content":"hi"}],"logprobs":true}`,
		`{"messages":[{"role":"user","content":"hi"}],"top_p":2}`,
		`{"messages":[{"role":"user","content":"hi"}],"temperature":-1}`,
		`{"messages":[{"role":"user","content":"hi"}],"stream_options":[]}`,
		`{"messages":[{"role":"user","content":"hi"}],"stream_options":{"include_usage":"true"}}`,
	} {
		t.Run(body, func(t *testing.T) {
			_, err := (Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "chatglm_turbo", Body: []byte(body)}, gateway.Target{BaseURL: "https://example.com", Secret: "id.secret"})
			var failure *gateway.UpstreamError
			if !errors.As(err, &failure) || failure.Code != "unsupported_request" || failure.Status != http.StatusBadRequest {
				t.Fatalf("error = %v", err)
			}
		})
	}
	_, err := (Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolResponses}, gateway.Target{})
	var failure *gateway.UpstreamError
	if !errors.As(err, &failure) || failure.Code != "unsupported_protocol" {
		t.Fatalf("protocol error = %v", err)
	}
}
