package bridge

import (
	"io"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func BenchmarkBridgeStreaming(b *testing.B) {
	for _, tc := range []struct {
		name     string
		protocol gateway.Protocol
	}{{"Responses", gateway.ProtocolResponses}, {"Anthropic", gateway.ProtocolAnthropic}, {"Gemini", gateway.ProtocolGemini}} {
		b.Run(tc.name, func(b *testing.B) {
			req := request(tc.protocol, true)
			usage := &gateway.Usage{PromptTokens: 100, CompletionTokens: 100}
			b.ReportAllocs()
			for b.Loop() {
				events := make([]gateway.Event, 0, 102)
				for range 100 {
					events = append(events, chunk(`{"content":"delta"}`, nil))
				}
				events = append(events, gateway.Event{Kind: gateway.EventUsage, Usage: usage}, gateway.Event{Kind: gateway.EventDone})
				stream := newStream(req, &fakeStream{events: events})
				for {
					_, err := stream.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
