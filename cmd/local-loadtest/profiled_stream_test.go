package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProfiledStreamValidationRejectsPartialOutputAndWrongUsage(t *testing.T) {
	for _, protocol := range []string{"chat", "responses"} {
		t.Run(protocol, func(t *testing.T) {
			profile := profiledWorkload{Protocol: protocol, Model: "gpt-6.1-sol", InputTokens: 16, CachedTokens: 12, OutputTokens: 64, ChunkTokens: 8}
			for _, partial := range []bool{false, true} {
				upstream := profile
				if partial {
					upstream.DropEvery = 1
				}
				recorder := httptest.NewRecorder()
				started := time.Now()
				returnProfiledStream(recorder, httptest.NewRequest("POST", "/", nil), 0, 0, upstream, 1)
				obs := readProfiledStream(strings.NewReader(recorder.Body.String()), started, profile)
				if obs.OK == partial {
					t.Fatalf("partial=%v incorrectly classified: %+v", partial, obs)
				}
				if partial && obs.Error == "" {
					t.Fatal("partial stream must report an error")
				}
				wrongUsage := profile
				wrongUsage.CachedTokens = 0
				if readProfiledStream(strings.NewReader(recorder.Body.String()), started, wrongUsage).OK {
					t.Fatal("incorrect cache accounting was counted as success")
				}
			}
		})
	}
}
