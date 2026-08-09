package stream

import (
	"time"

	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
)

type Result struct {
	status     *gatewaycontract.StreamStatus
	stopped    bool
	receivedAt time.Time
}

func newResult(status *gatewaycontract.StreamStatus) *Result {
	return &Result{status: status}
}

func (r *Result) Error(err error) {
	if err == nil {
		return
	}
	r.status.RecordError(err.Error())
}

func (r *Result) Stop(err error) {
	if err != nil {
		r.status.RecordError(err.Error())
	}
	r.status.SetEndReason(gatewaycontract.StreamEndReasonHandlerStop, err)
	r.stopped = true
}

func (r *Result) Done() {
	r.status.SetEndReason(gatewaycontract.StreamEndReasonDone, nil)
	r.stopped = true
}

func (r *Result) IsStopped() bool {
	return r.stopped
}

// ReceivedAt is the instant when the scanner read this SSE frame from the
// upstream response body. It excludes downstream handler scheduling time.
func (r *Result) ReceivedAt() time.Time {
	return r.receivedAt
}

func (r *Result) setReceivedAt(receivedAt time.Time) {
	r.receivedAt = receivedAt
}

func (r *Result) reset() {
	r.stopped = false
	r.receivedAt = time.Time{}
}
