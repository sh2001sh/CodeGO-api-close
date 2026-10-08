package codex_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/codex"
)

func TestCodexFastActualTierSurvivesAllResponseForms(t *testing.T) {
	for _, protocol := range []gateway.Protocol{gateway.ProtocolResponses, gateway.ProtocolOpenAIChat} {
		for _, stream := range []bool{false, true} {
			for _, reportedUsage := range []bool{false, true} {
				for _, tier := range []string{"fast", "priority", "default"} {
					t.Run(fmt.Sprintf("protocol%d/stream%t/usage%t/%s", protocol, stream, reportedUsage, tier), func(t *testing.T) {
						usage := ""
						if reportedUsage {
							usage = `,"usage":{"input_tokens":5,"output_tokens":2}`
						}
						response := `{"status":"completed","service_tier":"` + tier + `","output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}]` + usage + `}`
						body := response
						header := http.Header{}
						if stream {
							header.Set("Content-Type", "text/event-stream")
							body = "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + response + "}\n\n"
						}
						s := (codex.Provider{}).Decode(&gateway.Request{Protocol: protocol, Stream: stream, Body: []byte(`{}`)}, &http.Response{Header: header, Body: io.NopCloser(strings.NewReader(body))})
						defer func() { _ = s.Close() }()
						found := false
						for {
							ev, err := s.Next()
							if err == io.EOF {
								break
							}
							if err != nil || ev.Kind == gateway.EventError {
								t.Fatalf("decode event=%+v err=%v", ev, err)
							}
							if ev.ServiceTier == tier {
								found = true
							}
							if ev.Usage != nil && ev.Usage.ServiceTier != tier {
								t.Fatalf("usage lost actual tier: %+v", ev.Usage)
							}
						}
						if !found {
							t.Fatal("actual service tier was lost")
						}
					})
				}
			}
		}
	}
}
