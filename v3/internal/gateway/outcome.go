package gateway

import "time"

// Terminal is the single end state of a request (plan §4 decision table).
// Exactly one is chosen per request, in one place, and billing follows it.
type Terminal uint8

const (
	TerminalCompleted                 Terminal = iota + 1 // settle actual usage
	TerminalCompletedNoUsage                              // settle estimated usage, flagged
	TerminalUpstreamErrorBeforeOutput                     // full refund
	TerminalUpstreamErrorAfterOutput                      // settle what was delivered
	TerminalEmptyStream                                   // full refund
	TerminalClientCanceled                                // settle what was consumed
	TerminalTimeout                                       // settle consumed; refund if nothing delivered
)

var terminalNames = map[Terminal]string{
	TerminalCompleted:                 "completed",
	TerminalCompletedNoUsage:          "completed_no_usage",
	TerminalUpstreamErrorBeforeOutput: "upstream_error_before_output",
	TerminalUpstreamErrorAfterOutput:  "upstream_error_after_output",
	TerminalEmptyStream:               "empty_stream",
	TerminalClientCanceled:            "client_canceled",
	TerminalTimeout:                   "timeout",
}

func (t Terminal) String() string {
	if name, ok := terminalNames[t]; ok {
		return name
	}
	return "unknown"
}

// Outcome is what Finalize hands to billing and audit.
type Outcome struct {
	Terminal  Terminal
	Delivered bool  // at least one data event reached the client
	Usage     Usage // zero when Charge is false
	Charge    bool  // false means release the whole reservation
	Target    *Target
	Err       *UpstreamError // last upstream error, if any
}

// decide maps what happened on the final attempt to the terminal state and
// billing action. It is the only place that makes this decision.
func decide(f finish) Outcome {
	out := Outcome{Delivered: f.delivered, Err: f.err}
	switch {
	case f.timedOut:
		out.Terminal = TerminalTimeout
	case f.clientGone:
		out.Terminal = TerminalClientCanceled
	case f.empty:
		out.Terminal = TerminalEmptyStream
	case f.err != nil && f.delivered:
		out.Terminal = TerminalUpstreamErrorAfterOutput
	case f.err != nil:
		out.Terminal = TerminalUpstreamErrorBeforeOutput
	case !f.delivered:
		out.Terminal = TerminalEmptyStream
	case f.usage != nil && !f.usage.Estimated:
		out.Terminal = TerminalCompleted
	default:
		out.Terminal = TerminalCompletedNoUsage
	}

	// Anything reported by the upstream wins over a local estimate, whatever
	// the chargeable terminal: a canceled stream that was drained has usage.
	switch {
	case out.Terminal == TerminalEmptyStream || out.Terminal == TerminalUpstreamErrorBeforeOutput || (out.Terminal == TerminalTimeout && !out.Delivered):
		out.Charge = false
	case f.usage != nil:
		out.Usage, out.Charge = *f.usage, true
		if f.usage.Estimated {
			out.Usage.PromptTokens = f.estimate.PromptTokens
			out.Usage.CompletionTokens = f.estimate.CompletionTokens
		}
	case f.delivered || (f.clientGone && f.estimate.CompletionTokens > 0):
		out.Usage, out.Charge = f.estimate, true
	default:
		out.Charge = false // nothing reached the client and nothing was reported
	}
	return out
}

// finish is the raw observation of one attempt, fed to decide.
type finish struct {
	ttft       time.Duration // first upstream data event, separate from full relay duration
	delivered  bool
	usage      *Usage // upstream-reported
	estimate   Usage  // local estimate of what was delivered
	err        *UpstreamError
	empty      bool // HTTP 200 but no data event before the stream ended
	clientGone bool
	timedOut   bool
}
