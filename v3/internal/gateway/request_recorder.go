package gateway

// RequestRecorder receives terminal metadata, independently of optional body
// sampling. Implementations must snapshot identifiers and return without I/O.
type RequestRecorder interface {
	RecordRequest(*Request, Outcome, bool)
}

// RecordRequest includes whether settlement accepted the outcome. A billing
// outage does not rewrite the observed upstream success or failure.
func RecordRequest(rec RequestRecorder, req *Request, out Outcome, settled bool) {
	if rec != nil {
		rec.RecordRequest(req, out, settled)
	}
}

func (g *Gateway) recordRejected(req *Request, status int, code string) {
	RecordRequest(g.requests, req, Outcome{Err: &UpstreamError{Status: status, Code: code}}, false)
}
