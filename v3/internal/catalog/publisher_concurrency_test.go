package catalog

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestPublishCoalescesChangesDuringSlowPublication(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := &Publisher{pending: true, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		started := make(chan int)
		release := make(chan struct{})
		done := make(chan struct{})
		calls := 0
		go func() {
			defer close(done)
			p.publishPending(func() error {
				calls++
				started <- calls
				if calls == 1 {
					<-release
				}
				return nil
			})
		}()
		if got := <-started; got != 1 {
			t.Fatalf("first publish=%d", got)
		}
		var wg sync.WaitGroup
		for range 1000 {
			wg.Go(func() { p.Publish(context.Background()) })
		}
		wg.Wait()
		// The old implementation cleared pending before publishing, so this
		// burst scheduled more publishers while the first held a connection.
		synctest.Wait()
		p.mu.Lock()
		pending, dirty := p.pending, p.dirty
		p.mu.Unlock()
		if !pending || !dirty || calls != 1 {
			t.Fatalf("blocked publisher: pending=%v dirty=%v calls=%d", pending, dirty, calls)
		}
		close(release)
		if got := <-started; got != 2 {
			t.Fatalf("follow-up publish=%d", got)
		}
		<-done
		if calls != 2 || p.pending || p.dirty {
			t.Fatalf("finished: calls=%d pending=%v dirty=%v", calls, p.pending, p.dirty)
		}
	})
}

func TestPublishRetriesFailureWithoutAnotherInvalidation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := &Publisher{pending: true, dirty: true, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		calls, started := 0, time.Now()
		p.publishPending(func() error {
			calls++
			if calls == 1 {
				return errors.New("temporary publication failure")
			}
			return nil
		})
		if calls != 2 || p.pending || p.dirty || time.Since(started) != publishRetryDelay {
			t.Fatalf("failed invalidation lost or retry spun: calls=%d pending=%v dirty=%v elapsed=%v", calls, p.pending, p.dirty, time.Since(started))
		}
	})
}

func TestPublishNowCanceledWhileWaitingDoesNotAcquireConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := &Publisher{publishGate: make(chan struct{}, 1)}
		p.publishGate <- struct{}{}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := p.PublishNow(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("waiting publish error=%v", err)
		}
		if len(p.publishGate) != 1 {
			t.Fatal("canceled waiter released another publisher's gate")
		}
	})
}
