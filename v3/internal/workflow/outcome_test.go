package workflow

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestAsyncTerminalUsesSharedGatewayDecision(t *testing.T) {
	for _, tc := range []struct {
		result   native.Result
		terminal gateway.Terminal
		charge   bool
	}{
		{native.Result{Status: "failed", Units: 8, Usage: gateway.Usage{PromptTokens: 9}}, gateway.TerminalUpstreamErrorBeforeOutput, false},
		{native.Result{Status: "completed", Units: 8}, gateway.TerminalCompleted, true},
		{native.Result{Status: "completed"}, gateway.TerminalCompletedNoUsage, true},
		{native.Result{Status: "completed", Usage: gateway.Usage{ToolCalls: map[string]int64{"web_search": 1}}}, gateway.TerminalCompleted, true},
	} {
		out := OutcomeFor(tc.result, nil)
		if out.Terminal != tc.terminal || out.Charge != tc.charge {
			t.Fatalf("async terminal %s: %+v", tc.result.Status, out)
		}
		if !out.Charge && out.Usage.PromptTokens != 0 {
			t.Fatal("failed task retained billable usage")
		}
		if tc.result.Usage.ToolCalls != nil && out.Usage.ToolCalls["web_search"] != 1 {
			t.Fatal("tool-only completion lost reported usage")
		}
	}
}
