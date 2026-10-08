package gateway_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/codex"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
)

// Byte-sized synthetic inputs avoid dependence on a tokenizer or paid upstream.
func BenchmarkRequestPreparation(b *testing.B) {
	for _, size := range []struct {
		name  string
		bytes int
	}{{"4KiB", 4 << 10}, {"1MiB", 1 << 20}} {
		for _, tc := range []struct {
			name     string
			provider gateway.Provider
			secret   string
			fast     bool
		}{
			{"responses", responses.Provider{}, "synthetic", false},
			{"responses-fast", responses.Provider{}, "synthetic", true},
			{"codex", codex.Provider{}, `{"access_token":"synthetic","account_id":"synthetic"}`, false},
			{"codex-fast", codex.Provider{}, `{"access_token":"synthetic","account_id":"synthetic"}`, true},
		} {
			b.Run(size.name+"/"+tc.name, func(b *testing.B) {
				tier := ""
				if tc.fast {
					tier = `,"service_tier":"fast"`
				}
				body := []byte(`{"model":"model","stream":true,"input":"` + strings.Repeat("x", size.bytes) + `"` + tier + `}`)
				req := &gateway.Request{Protocol: gateway.ProtocolResponses, Path: "/v1/responses", Model: "model", Stream: true, Body: body}
				target := gateway.Target{BaseURL: "https://upstream.invalid", Secret: tc.secret}
				b.ReportAllocs()
				b.SetBytes(int64(len(body)))
				for b.Loop() {
					out, err := gateway.BuildProviderRequest(context.Background(), tc.provider, req, target)
					if err != nil {
						b.Fatal(err)
					}
					if err := gateway.ApplyUpstreamRequest(out, req, target); err != nil {
						b.Fatal(err)
					}
					if err := out.Body.Close(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
