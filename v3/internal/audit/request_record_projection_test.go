package audit

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestRequestSummaryTerminalAndLegacyCountingRules(t *testing.T) {
	for _, tc := range []struct {
		terminal gateway.Terminal
		status   string
		counted  bool
		code     int64
	}{
		{gateway.TerminalCompleted, "success", true, 200},
		{gateway.TerminalCompletedNoUsage, "success", true, 200},
		{gateway.TerminalEmptyStream, "failed", true, 502},
		{gateway.TerminalClientCanceled, "cancelled", false, 499},
		{gateway.TerminalTimeout, "failed", true, 504},
	} {
		req := summaryRequest("terminal")
		req.Attempts = []gateway.Attempt{{}, {}}
		got := ProjectRequestRecord(req, gateway.Outcome{Terminal: tc.terminal, Charge: true}, false)
		if got.Status != tc.status || got.Counted != tc.counted || got.StatusCode != tc.code || got.Billable || got.Attempts != 2 || got.Retries != 1 {
			t.Fatalf("%s: %+v", tc.terminal, got)
		}
	}
	for _, status := range []int64{400, 402, 404, 405, 422, 499} {
		if countRequestFailure(status, "invalid_request") {
			t.Fatalf("legacy excluded status %d entered health denominator", status)
		}
	}
	for _, status := range []int64{0, 401, 403, 408, 409, 425, 429, 500, 502, 504} {
		if !countRequestFailure(status, "upstream_unavailable") {
			t.Fatalf("real failure status %d omitted", status)
		}
	}
	for _, code := range []string{"sensitive_words_detected", "cyber_policy", "client_canceled"} {
		if countRequestFailure(403, code) {
			t.Fatalf("local policy/cancellation %s entered route health", code)
		}
	}
}
