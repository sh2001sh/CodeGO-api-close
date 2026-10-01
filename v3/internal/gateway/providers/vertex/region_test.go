package vertex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestChannelRegionMetadataPreservesV2Selection(t *testing.T) {
	c := Credentials{ProjectID: "project", AccessToken: "token", Region: "us-west1",
		Regions: map[string]string{"client-alias": "us-west2", "default": "us-west3"}}
	for _, tc := range []struct {
		name     string
		settings map[string]any
		region   string
	}{
		{"absent-credential-client-fallback", nil, "us-west2"},
		{"nil-credential-client-fallback", map[string]any{"api_version": nil}, "us-west2"},
		{"plain-region", map[string]any{"api_version": "us-east5"}, "us-east5"},
		{"trimmed-region", map[string]any{"api_version": "  europe-west4  "}, "europe-west4"},
		{"plain-global", map[string]any{"api_version": "global"}, "global"},
		{"empty-global", map[string]any{"api_version": ""}, "global"},
		{"blank-global", map[string]any{"api_version": "  "}, "global"},
		{"client-before-upstream", map[string]any{"api_version": `{"client-alias":"europe-west4","gemini-2.5-pro":"asia-northeast1","default":"us-east1"}`}, "europe-west4"},
		{"default-region", map[string]any{"api_version": `{"another-model":"europe-west4","default":"us-east1"}`}, "us-east1"},
		{"map-missing-default-global", map[string]any{"api_version": `{"another-model":"europe-west4"}`}, "global"},
		{"empty-map-global", map[string]any{"api_version": `{}`}, "global"},
		{"empty-client-global", map[string]any{"api_version": `{"client-alias":"","default":"us-east1"}`}, "global"},
		{"empty-default-global", map[string]any{"api_version": `{"default":""}`}, "global"},
		{"map-value-trimmed", map[string]any{"api_version": `{"client-alias":"  us-east5  "}`}, "us-east5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := gateway.Target{Secret: encoded(t, c), UpstreamModel: "gemini-2.5-pro", Settings: tc.settings}
			out, err := (Provider{}).BuildRequest(context.Background(), &gateway.Request{Model: "client-alias", Protocol: gateway.ProtocolGemini, Body: []byte(`{"contents":[]}`)}, target)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = out.Body.Close() }()
			host := "aiplatform.googleapis.com"
			if tc.region != "global" {
				host = tc.region + "-" + host
			}
			if out.URL.Host != host || out.URL.Path != "/v1/projects/project/locations/"+tc.region+"/publishers/google/models/gemini-2.5-pro:generateContent" {
				t.Fatalf("wrong region/model routing: %s", out.URL)
			}
			if target.Secret != encoded(t, c) || c.Region != "us-west1" || c.Regions["client-alias"] != "us-west2" {
				t.Fatal("channel routing mutated credentials")
			}
			if tc.settings != nil {
				if value, exists := tc.settings["api_version"]; !exists || target.Settings["api_version"] != value {
					t.Fatal("delegate removed region metadata from frozen target")
				}
			}
		})
	}
}

func TestInvalidChannelRegionsFailAs502BeforeOAuth(t *testing.T) {
	c, _ := testAccount(t)
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"access_token":"token","expires_in":3600}`)
	}))
	defer server.Close()
	p := Provider{HTTPClient: server.Client(), TokenEndpoint: server.URL}
	for _, value := range []any{
		true, 42, map[string]string{"default": "us-east5"}, map[string]any{}, []string{"us-east5"},
		`{"default":42}`, `{"default":null}`, `{"client-alias":[]}`, `{"unselected":false,"default":"us-east5"}`,
		`{"client-alias":"us-east5","unused":"region/SECRET-VALUE"}`, `{"default":"us-east5"`,
		`["us-east5"]`, `"us-east5"`, "null", "true", "us-east5/SECRET-VALUE", "us-east5?key=SECRET-VALUE", "us-east5\r\nSECRET-VALUE", "US-EAST5", "us-east5.example", "us-east5@host",
	} {
		_, err := p.BuildRequest(context.Background(), &gateway.Request{Model: "client-alias", Protocol: gateway.ProtocolGemini, Body: []byte(`{"contents":[]}`)},
			gateway.Target{Secret: encoded(t, c), UpstreamModel: "gemini", Settings: map[string]any{"api_version": value}})
		var upstream *gateway.UpstreamError
		if !errors.As(err, &upstream) || upstream.Status != http.StatusBadGateway || upstream.Code != "invalid_channel_config" || strings.Contains(err.Error(), "SECRET-VALUE") || strings.Contains(err.Error(), c.ClientEmail) || strings.Contains(err.Error(), "PRIVATE KEY") {
			t.Fatalf("invalid settings did not produce sanitized 502: %v", err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid channel configuration reached OAuth endpoint %d times", calls.Load())
	}
}

func TestAuxiliaryNativeActionUsesChannelRegionMetadata(t *testing.T) {
	req := &gateway.Request{Model: "embed-alias", Protocol: gateway.ProtocolOpenAIChat, Body: []byte(`{"content":{"parts":[{"text":"hi"}]}}`)}
	target := gateway.Target{BaseURL: "https://custom.example/prefix", Secret: encoded(t, Credentials{ProjectID: "project", AccessToken: "token", Region: "us-west1"}),
		UpstreamModel: "gemini-embedding-001", Settings: map[string]any{"api_version": `{"embed-alias":"us-east5","gemini-embedding-001":"us-east1"}`}}
	out, err := (Provider{}).BuildActionRequest(context.Background(), req, target, "embedContent")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()
	body, _ := io.ReadAll(out.Body)
	if out.URL.Host != "custom.example" || out.URL.Path != "/prefix/v1/projects/project/locations/us-east5/publishers/google/models/gemini-embedding-001:embedContent" || string(body) != string(req.Body) {
		t.Fatalf("native action lost channel region/alias: %s %s", out.URL, body)
	}
}
