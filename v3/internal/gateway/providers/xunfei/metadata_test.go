package xunfei

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestChannelAPIVersionDrivesNativeEndpointAndDomain(t *testing.T) {
	for _, model := range []string{"SparkDesk", "SparkDesk-v1.1"} {
		for _, base := range []string{"https://spark.example.test", "https://spark.example.test/prefix/v2.1/chat?feature=keep"} {
			req := chatRequest(false, `{"messages":[{"role":"user","content":"hello"}]}`)
			req.Model = model
			target := gateway.Target{Secret: "test-app|test-secret|test-key", BaseURL: base, Settings: map[string]any{"api_version": "v3.5"}}
			upstream, err := (Provider{}).BuildRequest(context.Background(), req, target)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(upstream.Body)
			_ = upstream.Body.Close()
			path := "/v3.5/chat"
			if strings.Contains(base, "prefix") {
				path = "/prefix/v3.5/chat"
			}
			if upstream.URL.Path != path || gjson.GetBytes(body, "parameter.chat.domain").Str != "generalv3.5" {
				t.Fatalf("channel API version lost: %s %s", upstream.URL.Path, body)
			}
			if strings.Contains(base, "feature") && upstream.URL.Query().Get("feature") != "keep" {
				t.Fatal("custom endpoint query lost")
			}
		}
	}
}

func TestClientAPIVersionOverridesChannelVersionWithoutLosingPrefix(t *testing.T) {
	req := chatRequest(false, `{"messages":[{"role":"user","content":"hello"}]}`)
	req.ClientHeaders = map[string]string{"X-Spark-Api-Version": "v4.0"}
	upstream, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{Secret: "app|secret|key", BaseURL: "https://spark.example.test/relay/v1.1/chat?feature=1", Settings: map[string]any{"api_version": "v3.5"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(upstream.Body)
	_ = upstream.Body.Close()
	if upstream.URL.Path != "/relay/v4.0/chat" || gjson.GetBytes(body, "parameter.chat.domain").Str != "4.0Ultra" || upstream.URL.Query().Get("feature") != "1" {
		t.Fatalf("client override or custom prefix lost: %s %s", upstream.URL.Path, body)
	}
}

func TestInvalidChannelAPIVersionIsSafeRetryableConfigurationError(t *testing.T) {
	for _, version := range []any{nil, 35, true, []string{"v3.5"}, "invalid-sensitive-value", "v3.5/../../bad", " v3.5 "} {
		_, err := (Provider{}).BuildRequest(context.Background(), chatRequest(false, `{"messages":[{"role":"user","content":"hello"}]}`), gateway.Target{Secret: "app|secret|key", Settings: map[string]any{"api_version": version}})
		var failure *gateway.UpstreamError
		if !errors.As(err, &failure) || failure.Status != http.StatusBadGateway || failure.Type != "upstream_error" || strings.Contains(err.Error(), "sensitive") {
			t.Fatalf("channel version accepted or exposed configuration: %v", err)
		}
	}
}

func TestInvalidClientAPIVersionRemainsCallerError(t *testing.T) {
	req := chatRequest(false, `{"messages":[{"role":"user","content":"hello"}]}`)
	req.ClientHeaders = map[string]string{"X-Spark-Api-Version": "invalid-sensitive-value"}
	_, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{Secret: "app|secret|key", Settings: map[string]any{"api_version": "v3.5"}})
	var failure *gateway.UpstreamError
	if !errors.As(err, &failure) || failure.Status != http.StatusBadRequest || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("wrong client version classification: %v", err)
	}
}

func TestEmptyChannelAPIVersionRetainsModelInferenceAndFixedCustomPath(t *testing.T) {
	upstream, err := (Provider{}).BuildRequest(context.Background(), chatRequest(false, `{"messages":[{"role":"user","content":"hello"}]}`), gateway.Target{Secret: "app|secret|key", BaseURL: "https://spark.example.test/custom/chat", Settings: map[string]any{"api_version": ""}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(upstream.Body)
	_ = upstream.Body.Close()
	if upstream.URL.Path != "/custom/chat" || gjson.GetBytes(body, "parameter.chat.domain").Str != "generalv3.5" {
		t.Fatalf("empty metadata changed established custom endpoint: %s %s", upstream.URL.Path, body)
	}
}
