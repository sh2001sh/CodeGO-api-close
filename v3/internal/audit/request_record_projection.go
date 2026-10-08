package audit

import (
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func ProjectRequestRecord(req *gateway.Request, out gateway.Outcome, settled bool) RequestRecord {
	r := RequestRecord{RequestID: req.ID, UserID: req.Principal.UserID, KeyID: req.Principal.KeyID,
		Model: req.Model, Group: req.Principal.Group, StartedAt: req.Received, CompletedAt: time.Now().UTC(),
		Attempts: int64(len(req.Attempts)), Retries: int64(max(len(req.Attempts)-1, 0)),
		PromptTokens: out.Usage.PromptTokens, CompletionTokens: out.Usage.CompletionTokens,
		Billable: out.Charge && settled, RequestType: "sync", Status: "failed", StatusCode: http.StatusBadGateway}
	if req.Stream && out.Delivered && out.TTFT > 0 {
		ttft := float64(out.TTFT) / float64(time.Millisecond)
		r.TTFTMS = &ttft
		// Billing estimates and interrupted/drained output are not throughput
		// measurements. Historical accounting tokens remain unchanged.
		if out.Terminal == gateway.TerminalCompleted && !out.Usage.Estimated &&
			out.Usage.CompletionTokens > 0 && out.Generation > 0 {
			generation := float64(out.Generation) / float64(time.Millisecond)
			r.GenerationMS = &generation
		}
	}
	if settled {
		r.Amount = atomic.LoadInt64(&req.SettledAmount)
	}
	if req.PersistedAttempts > 0 {
		r.Attempts, r.Retries = req.PersistedAttempts, req.PersistedAttempts-1
	}
	if req.Stream {
		r.RequestType = "stream"
	}
	if strings.HasPrefix(req.ID, "resp_bg_") {
		r.RequestType = "async"
	}
	switch req.Protocol {
	case gateway.ProtocolOpenAIChat:
		r.Protocol = "openai_chat"
	case gateway.ProtocolResponses:
		r.Protocol = "responses"
	case gateway.ProtocolAnthropic:
		r.Protocol = "anthropic"
	case gateway.ProtocolGemini:
		r.Protocol = "gemini"
	}
	if out.Target != nil {
		if req.Protocol == 0 {
			r.Protocol, r.RequestType = out.Target.Provider, "async"
		}
		r.ChannelID = out.Target.ChannelID
		if out.Target.Group != "" {
			r.Group = out.Target.Group
		}
	}
	if out.Err != nil {
		r.StatusCode, r.ErrorCode = int64(out.Err.Status), out.Err.Code
	}
	switch out.Terminal {
	case gateway.TerminalCompleted, gateway.TerminalCompletedNoUsage:
		r.Status, r.StatusCode, r.Counted = "success", http.StatusOK, true
	case gateway.TerminalClientCanceled:
		r.Status, r.StatusCode, r.ErrorCode = "cancelled", 499, "client_canceled"
	case gateway.TerminalTimeout:
		r.StatusCode, r.ErrorCode, r.Counted = http.StatusGatewayTimeout, "upstream_timeout", true
	case gateway.TerminalEmptyStream:
		r.StatusCode, r.ErrorCode, r.Counted = http.StatusBadGateway, "empty_stream", true
	default:
		r.Counted = countRequestFailure(r.StatusCode, r.ErrorCode)
		if out.Terminal == 0 {
			r.Status = "rejected"
		}
	}
	return r
}

// Keep v2's client/policy error exclusions while recording all terminal facts.
// Client disconnects are explicitly excluded: they do not measure route health.
func countRequestFailure(status int64, code string) bool {
	if code == "sensitive_words_detected" || code == "cyber_policy" || code == "client_canceled" {
		return false
	}
	return status == 0 || status == 408 || status == 409 || status == 425 || status == 429 || status == 401 || status == 403 || status >= 500
}
