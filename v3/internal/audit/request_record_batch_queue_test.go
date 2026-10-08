package audit

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type batchSummarySink struct {
	singleCalls atomic.Int64
	write       func(context.Context, []RequestRecord) error
}

func (s *batchSummarySink) RecordRequest(context.Context, RequestRecord) error {
	s.singleCalls.Add(1)
	return errors.New("single-row writes must not be used by a batch store")
}

func (s *batchSummarySink) RecordRequests(ctx context.Context, records []RequestRecord) error {
	return s.write(ctx, records)
}

func TestRequestSummaryBatchesBurstWithoutDroppingAndFlushesOnClose(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var observed, largest atomic.Int64
	sink := &batchSummarySink{write: func(ctx context.Context, records []RequestRecord) error {
		once.Do(func() { close(started) })
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		observed.Add(int64(len(records)))
		for previous := largest.Load(); int64(len(records)) > previous; previous = largest.Load() {
			if largest.CompareAndSwap(previous, int64(len(records))) {
				break
			}
		}
		return nil
	}}
	s := startRequestRecorder(context.Background(), sink, requestTestLog(io.Discard), 4096)
	s.RecordRequest(summaryRequest("first"), gateway.Outcome{}, false)
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		s.Close()
		t.Fatal("batch-capable store was never called")
	}
	for range 2048 {
		s.RecordRequest(summaryRequest("queued"), gateway.Outcome{}, false)
	}
	close(release)
	s.Close()
	if sink.singleCalls.Load() != 0 || observed.Load() != 2049 || largest.Load() <= 1 || largest.Load() > 128 || s.written.Load() != 2049 || s.failed.Load() != 0 || s.dropped.Load() != 0 {
		t.Fatalf("burst incomplete or unbounded: single=%d observed=%d largest=%d written=%d failed=%d dropped=%d", sink.singleCalls.Load(), observed.Load(), largest.Load(), s.written.Load(), s.failed.Load(), s.dropped.Load())
	}
}

func TestRequestSummaryBatchFailureCountsEveryObservation(t *testing.T) {
	sink := &batchSummarySink{write: func(context.Context, []RequestRecord) error { return errors.New("database unavailable") }}
	s := startRequestRecorder(context.Background(), sink, requestTestLog(io.Discard), 16)
	for range 10 {
		s.RecordRequest(summaryRequest("queued"), gateway.Outcome{}, false)
	}
	s.Close()
	if s.failed.Load() != 10 || s.written.Load() != 0 || s.dropped.Load() != 0 || sink.singleCalls.Load() != 0 {
		t.Fatalf("failed batch not accounted: failed=%d written=%d dropped=%d single=%d", s.failed.Load(), s.written.Load(), s.dropped.Load(), sink.singleCalls.Load())
	}
}

func TestRequestSummaryConcurrentBatchesAccountEveryObservation(t *testing.T) {
	var observed atomic.Int64
	sink := &batchSummarySink{write: func(_ context.Context, records []RequestRecord) error {
		observed.Add(int64(len(records)))
		return nil
	}}
	s := startRequestRecorder(context.Background(), sink, requestTestLog(io.Discard), 4096)
	var producers sync.WaitGroup
	for range 32 {
		producers.Add(1)
		go func() {
			defer producers.Done()
			for range 50 {
				s.RecordRequest(summaryRequest("concurrent-batch"), gateway.Outcome{}, false)
			}
		}()
	}
	producers.Wait()
	s.Close()
	if observed.Load() != 1600 || s.written.Load() != 1600 || s.failed.Load() != 0 || s.dropped.Load() != 0 || sink.singleCalls.Load() != 0 {
		t.Fatalf("concurrent metadata lost: observed=%d written=%d failed=%d dropped=%d single=%d", observed.Load(), s.written.Load(), s.failed.Load(), s.dropped.Load(), sink.singleCalls.Load())
	}
}

func TestRequestSummaryBlockedBatchShutdownHasBoundedDrain(t *testing.T) {
	sink := &batchSummarySink{write: func(ctx context.Context, _ []RequestRecord) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	s := startRequestRecorder(context.Background(), sink, requestTestLog(io.Discard), 16)
	for range 10 {
		s.RecordRequest(summaryRequest("blocked-batch"), gateway.Outcome{}, false)
	}
	started := time.Now()
	s.Close()
	if time.Since(started) > requestRecordTimeout+time.Second || s.written.Load() != 0 || s.failed.Load()+s.dropped.Load() != 10 || sink.singleCalls.Load() != 0 {
		t.Fatalf("blocked batch shutdown lost accounting or exceeded bound: duration=%v written=%d failed=%d dropped=%d single=%d", time.Since(started), s.written.Load(), s.failed.Load(), s.dropped.Load(), sink.singleCalls.Load())
	}
}

func TestRequestRecordBatchRejectsOversizedAndConflictingInputBeforeStorage(t *testing.T) {
	s := New(nil, Config{})
	if err := s.RecordRequests(context.Background(), nil); err != nil {
		t.Fatal("empty batch should require no database", err)
	}
	if err := s.RecordRequests(context.Background(), make([]RequestRecord, 129)); !errors.Is(err, ErrInvalid) {
		t.Fatal("oversized batch was not rejected", err)
	}
	r := ProjectRequestRecord(summaryRequest("collision"), gateway.Outcome{}, false)
	other := r
	other.UserID++
	if err := s.RecordRequests(context.Background(), []RequestRecord{r, other}); err == nil || errors.Is(err, ErrUnavailable) {
		t.Fatal("same-batch identity conflict reached storage", err)
	}
}
