package gemini_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/gemini"
)

func TestGeminiVersionPreservesChannelSettingAndExplicitPrefix(t *testing.T) {
	for _, tc := range []struct {
		name, base, version, want string
	}{
		{"default", "https://google.example", "", "v1beta"},
		{"stable", "https://google.example", "v1", "v1"},
		{"beta", "https://google.example", "v1beta", "v1beta"},
		{"alpha", "https://google.example", "v1alpha", "v1alpha"},
		{"explicit stable", "https://google.example/v1/", "v1beta", "v1"},
		{"explicit beta", "https://google.example/v1beta", "v1", "v1beta"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := (gemini.Provider{}).BuildRequest(context.Background(), &gateway.Request{
				Protocol: gateway.ProtocolGemini, Model: "gemini-2.5-flash", Body: []byte(`{"contents":[]}`)}, gateway.Target{
				BaseURL: tc.base, Settings: map[string]any{"api_version": tc.version}})
			if err != nil || req.URL.Path != "/"+tc.want+"/models/gemini-2.5-flash:generateContent" {
				t.Fatalf("channel API version lost: %+v %v", req, err)
			}
		})
	}
}

func TestGeminiRejectsInvalidVersionConfigurationAsUpstreamFailure(t *testing.T) {
	for _, configured := range []any{42, true, []string{"v1"}, "../v1", "v1?key=x", "v1\n", " v1", "v9"} {
		_, err := (gemini.Provider{}).BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolGemini,
			Model: "gemini", Body: []byte(`{}`)}, gateway.Target{BaseURL: "https://google.example/v1", Settings: map[string]any{"api_version": configured}})
		var failure *gateway.UpstreamError
		if !errors.As(err, &failure) || failure.Status != http.StatusBadGateway || failure.Code != "invalid_channel_configuration" {
			t.Fatalf("invalid version silently accepted or blamed on caller: %v %v", configured, err)
		}
	}
}
