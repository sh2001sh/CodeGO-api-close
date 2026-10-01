package bedrock

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

func TestBuildClaudeRequestsAndLegacyModelAliases(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, protocol := range []gateway.Protocol{gateway.ProtocolOpenAIChat, gateway.ProtocolAnthropic} {
			req := &gateway.Request{Protocol: protocol, Model: "claude-3-5-sonnet-20240620", Stream: streaming,
				Body: []byte(`{"model":"claude-3-5-sonnet-20240620","max_tokens":128,"messages":[{"role":"user","content":"hello"}],"stream":false}`)}
			target := gateway.Target{Secret: "test-bearer|us-east-1"}
			out, err := (Provider{}).BuildRequest(context.Background(), req, target)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(out.Body)
			if err != nil {
				t.Fatal(err)
			}
			_ = out.Body.Close()
			action := "invoke"
			if streaming {
				action = "invoke-with-response-stream"
			}
			if out.URL.Host != "bedrock-runtime.us-east-1.amazonaws.com" || !strings.HasSuffix(out.URL.Path, "/us.anthropic.claude-3-5-sonnet-20240620-v1:0/"+action) {
				t.Fatalf("wrong endpoint: %s", out.URL)
			}
			if gjson.GetBytes(body, "model").Exists() || gjson.GetBytes(body, "stream").Exists() || gjson.GetBytes(body, "anthropic_version").Str != "bedrock-2023-05-31" {
				t.Fatalf("invalid Bedrock body: %s", body)
			}
			if gjson.GetBytes(body, "max_tokens").Int() != 128 || out.Header.Get("Authorization") != "Bearer test-bearer" {
				t.Fatal("request content/authentication lost")
			}
			if streaming && out.Header.Get("Accept") != "application/vnd.amazon.eventstream" {
				t.Fatal("missing stream negotiation")
			}
		}
	}
}

func TestBuildARNModelAndSTSRequest(t *testing.T) {
	req := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "client-model", Body: []byte(`{"model":"client-model","messages":[{"role":"user","content":"hello"}]}`)}
	arn := "arn:aws:bedrock:us-east-1:123:model/test"
	out, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{Secret: `{"access_key_id":"test","secret_access_key":"secret","session_token":"session","region":"us-east-1"}`, BaseURL: "https://example.com/custom/?trace=two%20words", UpstreamModel: arn})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()
	if !strings.Contains(out.URL.EscapedPath(), "arn%3Aaws%3Abedrock%3Aus-east-1%3A123%3Amodel%2Ftest") || out.URL.Query().Get("trace") != "two words" {
		t.Fatalf("ARN/query corrupted: %s", out.URL)
	}
	if !strings.HasPrefix(out.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") || out.Header.Get("X-Amz-Security-Token") != "session" {
		t.Fatal("STS signing headers missing")
	}
}

func TestBuildRejectsUnsupportedNovaFeature(t *testing.T) {
	req := &gateway.Request{Protocol: gateway.ProtocolAnthropic, Model: "nova-pro-v1:0", Body: []byte(`{"model":"nova-pro-v1:0","max_tokens":128,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"enabled","budget_tokens":64}}`)}
	_, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{Secret: "test|us-east-1"})
	var upstream *gateway.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != http.StatusBadRequest {
		t.Fatalf("lossy feature did not return 400: %v", err)
	}
}

func TestBuildRejectsInvalidBase(t *testing.T) {
	req := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "model", Body: []byte(`{"messages":[{"role":"user","content":"hello"}]}`)}
	for _, base := range []string{"/relative", "ftp://example.com", "https://user:pass@example.com"} {
		_, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{Secret: "test|us-east-1", BaseURL: base})
		if err == nil {
			t.Fatalf("accepted invalid endpoint %s", base)
		}
	}
}

func TestModelAliasPreservesExplicitProfiles(t *testing.T) {
	for _, explicit := range []string{"us.anthropic.claude-3-haiku-20240307-v1:0", "arn:aws:bedrock:us-east-1:123:inference-profile/example", "custom-model"} {
		if modelID(explicit, "eu-west-1") != explicit {
			t.Fatalf("explicit ID overwritten: %s", explicit)
		}
	}
	if got := modelID("nova-pro-v1:0", "ap-northeast-1"); got != "apac.amazon.nova-pro-v1:0" {
		t.Fatal(got)
	}
}
