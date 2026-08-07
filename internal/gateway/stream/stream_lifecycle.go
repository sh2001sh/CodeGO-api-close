package stream

import (
	"net/http"
	"time"
)

func streamingFirstByteTimer(timer *time.Timer) <-chan time.Time {
	if timer == nil {
		return nil
	}
	return timer.C
}

func streamingMaxTimer(timer *time.Timer) <-chan time.Time {
	if timer == nil {
		return nil
	}
	return timer.C
}

// closeTimedOutStream interrupts Scanner.Scan immediately instead of waiting
// for an upstream connection that has already exceeded its stream budget.
func closeTimedOutStream(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_ = resp.Body.Close()
}
