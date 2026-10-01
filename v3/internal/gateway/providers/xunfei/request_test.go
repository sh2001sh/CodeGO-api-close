package xunfei

import (
	"context"
	"encoding/base64"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func chatRequest(stream bool, body string) *gateway.Request {
	return &gateway.Request{ID: "fixture", Protocol: gateway.ProtocolOpenAIChat, Model: "SparkDesk-v3.5", Stream: stream,
		Received: time.Unix(1700000000, 0), Body: []byte(body)}
}

func TestSignedURLMatchesIndependentFixture(t *testing.T) {
	u, _ := url.Parse("wss://spark-api.xf-yun.com/v3.5/chat?existing=preserved")
	signURL(u, "test-key", "test-secret", time.Date(2024, 10, 1, 0, 0, 0, 0, time.UTC))
	auth, err := base64.StdEncoding.DecodeString(u.Query().Get("authorization"))
	if err != nil {
		t.Fatal(err)
	}
	want := `hmac username="test-key", algorithm="hmac-sha256", headers="host date request-line", signature="nJQkAfOhDzTxjfMgA2pA34f/bVCVIQDmOUMePHSb7+Q="`
	if string(auth) != want || u.Query().Get("date") != "Tue, 01 Oct 2024 00:00:00 GMT" || u.Query().Get("existing") != "preserved" {
		t.Fatalf("incorrect authentication fixture: %q", auth)
	}
}

func TestBuildRequestPreservesCredentialOrderAndNativePayload(t *testing.T) {
	body := `{"model":"SparkDesk-v3.5","messages":[{"role":"system","content":"policy"},{"role":"user","content":[{"type":"text","text":"hello"}]}],"temperature":0.4,"max_tokens":30,"n":1}`
	req, err := (Provider{}).BuildRequest(context.Background(), chatRequest(true, body), gateway.Target{Secret: "test-app|test-secret|test-key", BaseURL: "ws://localhost:9000"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = req.Body.Close() }()
	native, _ := io.ReadAll(req.Body)
	if req.URL.Scheme != "http" || req.URL.Path != "/v3.5/chat" || req.Header.Get("Authorization") != "" {
		t.Fatalf("incorrect transport shape: %v", req.URL.Path)
	}
	if gjson.GetBytes(native, "header.app_id").Str != "test-app" || gjson.GetBytes(native, "parameter.chat.domain").Str != "generalv3.5" || gjson.GetBytes(native, "parameter.chat.max_tokens").Int() != 30 || gjson.GetBytes(native, "payload.message.text.0.role").Str != "system" || gjson.GetBytes(native, "payload.message.text.1.content").Str != "hello" {
		t.Fatalf("incorrect native payload: %s", native)
	}
	auth, _ := base64.StdEncoding.DecodeString(req.URL.Query().Get("authorization"))
	if !strings.Contains(string(auth), `username="test-key"`) || strings.Contains(string(auth), "test-secret") {
		t.Fatalf("incorrect credential order")
	}
}

func TestVersionDomainsAndLegacySystemConversion(t *testing.T) {
	for version, domain := range map[string]string{"v1.1": "lite", "v2.1": "generalv2", "v3.1": "generalv3", "v3.5": "generalv3.5", "v4.0": "4.0Ultra"} {
		t.Run(version, func(t *testing.T) {
			req := chatRequest(false, `{"messages":[{"role":"system","content":"policy"}],"max_completion_tokens":12}`)
			req.Model = "SparkDesk-" + version
			upstream, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{Secret: "app|secret|key"})
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(upstream.Body)
			_ = upstream.Body.Close()
			if upstream.URL.Path != "/"+version+"/chat" || gjson.GetBytes(body, "parameter.chat.domain").Str != domain || gjson.GetBytes(body, "parameter.chat.max_tokens").Int() != 12 {
				t.Fatalf("incorrect domain or path: %s", body)
			}
			role := "user"
			if version == "v3.5" {
				role = "system"
			}
			if gjson.GetBytes(body, "payload.message.text.0.role").Str != role {
				t.Fatalf("legacy system conversion: %s", body)
			}
		})
	}
}

func TestBuildRejectsLossyFeaturesAndInvalidCredentials(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"messages":[]}`, `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://image"}}]}]}`,
		`{"messages":[{"role":"tool","content":"hi"}]}`, `{"messages":[{"role":"user","content":"hi"}],"tools":[]}`,
		`{"messages":[{"role":"user","content":"hi"}],"n":2}`, `{"messages":[{"role":"user","content":"hi"}],"max_tokens":-1}`,
		`{"messages":[{"role":"user","content":"hi"}],"n":1.2}`,
	} {
		if _, err := (Provider{}).BuildRequest(context.Background(), chatRequest(false, body), gateway.Target{Secret: "app|secret|key"}); err == nil {
			t.Fatalf("accepted unsupported body: %s", body)
		}
	}
	for _, secret := range []string{"sensitive", "app||key", "app|secret|key|extra", "app|secret|key\"injected"} {
		_, err := (Provider{}).BuildRequest(context.Background(), chatRequest(false, `{"messages":[{"role":"user","content":"hi"}]}`), gateway.Target{Secret: secret})
		if err == nil || strings.Contains(err.Error(), secret) {
			t.Fatal("credential failure missing or leaked")
		}
	}
}

func TestExplicitVersionAndClientQueryVersionUseMatchingEndpoint(t *testing.T) {
	req := chatRequest(false, `{"messages":[{"role":"user","content":"hello"}]}`)
	upstream, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{Secret: "app|secret|key", BaseURL: "https://example.test/v4.0/chat"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(upstream.Body)
	_ = upstream.Body.Close()
	if gjson.GetBytes(body, "parameter.chat.domain").Str != "4.0Ultra" {
		t.Fatalf("configured endpoint domain mismatch: %s", body)
	}
	req.ClientHeaders = map[string]string{"X-Spark-Api-Version": "v2.1"}
	upstream, err = (Provider{}).BuildRequest(context.Background(), req, gateway.Target{Secret: "app|secret|key"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(upstream.Body)
	_ = upstream.Body.Close()
	if upstream.URL.Path != "/v2.1/chat" || gjson.GetBytes(body, "parameter.chat.domain").Str != "generalv2" {
		t.Fatalf("client version mismatch: %s", body)
	}
}

func TestCustomEndpointPreservesSignedPathAndQuery(t *testing.T) {
	req, err := (Provider{}).BuildRequest(context.Background(), chatRequest(false, `{"messages":[{"role":"user","content":"hello"}]}`), gateway.Target{Secret: "app|secret|key", BaseURL: "https://example.test/relay/chat?api-version=v4.0&feature=1"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = req.Body.Close() }()
	body, _ := io.ReadAll(req.Body)
	if req.URL.Path != "/relay/chat" || req.URL.Query().Get("feature") != "1" || req.URL.Query().Get("api-version") != "" || gjson.GetBytes(body, "parameter.chat.domain").Str != "4.0Ultra" {
		t.Fatalf("custom endpoint changed")
	}
}
