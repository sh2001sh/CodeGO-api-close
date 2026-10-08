package gateway

// Observation describes an exchange outside the text-stream pipeline, such as
// binary audio, a WebSocket session or a completed asynchronous media task.
// They share the same seven terminal states and billing decision as text.
type Observation struct {
	ServiceTier    string
	Delivered      bool
	Usage          *Usage
	Estimate       Usage
	Err            *UpstreamError
	Empty          bool
	ClientCanceled bool
	TimedOut       bool
}

// Decide applies the single terminal and charge decision to an observation.
func Decide(o Observation) Outcome {
	return decide(finish{delivered: o.Delivered, usage: o.Usage, estimate: o.Estimate,
		err: o.Err, empty: o.Empty, clientGone: o.ClientCanceled, timedOut: o.TimedOut, serviceTier: o.ServiceTier})
}
