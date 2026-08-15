package stream

import (
	"time"

	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
)

type Result struct {
	status     *gatewaycontract.StreamStatus
	stopped    bool
	receivedAt time.Time
	progress   func()
}

func newResult(status *gatewaycontract.StreamStatus, progress func()) *Result {
	return &Result{status: status, progress: progress}
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

// MarkProgress records a meaningful upstream increment for adaptive stream
// deadlines. Callers must use it only for text, reasoning, or tool deltas;
// lifecycle and heartbeat events do not count as progress.
func (r *Result) MarkProgress() {
	if r != nil && r.progress != nil {
		r.progress()
	}
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
