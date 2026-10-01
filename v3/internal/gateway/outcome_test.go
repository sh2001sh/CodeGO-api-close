package gateway

import "testing"

// Refund terminals cannot be overridden by a stray usage event received
// before an error or an empty completion.
func TestRefundTerminalsIgnoreReportedUsage(t *testing.T) {
	usage := &Usage{PromptTokens: 40, CompletionTokens: 20}
	for _, f := range []finish{
		{usage: usage, empty: true},
		{usage: usage, err: &UpstreamError{}},
		{usage: usage, timedOut: true},
	} {
		out := decide(f)
		if out.Charge {
			t.Fatalf("terminal %s charged usage %+v before output", out.Terminal, out.Usage)
		}
	}
}

func TestClientCanceledBeforeDeliveryStillBillsDrainedUsage(t *testing.T) {
	out := decide(finish{clientGone: true, usage: &Usage{PromptTokens: 10, CompletionTokens: 2}})
	if out.Terminal != TerminalClientCanceled || !out.Charge || out.Usage.CompletionTokens != 2 {
		t.Fatalf("outcome=%+v", out)
	}
}

func TestClientCanceledBillsBufferedConsumedOutput(t *testing.T) {
	out := decide(finish{clientGone: true, estimate: Usage{PromptTokens: 10, CompletionTokens: 4, Estimated: true}})
	if out.Terminal != TerminalClientCanceled || !out.Charge || out.Usage.CompletionTokens != 4 {
		t.Fatalf("outcome=%+v", out)
	}
}
