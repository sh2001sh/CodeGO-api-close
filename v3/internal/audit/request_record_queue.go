package audit

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type requestRecordStore interface {
	RecordRequest(context.Context, RequestRecord) error
}

type requestRecordBatchStore interface {
	RecordRequests(context.Context, []RequestRecord) error
}

type RequestRecorder struct {
	store                    requestRecordStore
	batchStore               requestRecordBatchStore
	log                      *slog.Logger
	queue                    chan RequestRecord
	done                     chan struct{}
	stop                     context.CancelFunc
	once                     sync.Once
	mu                       sync.RWMutex
	closed                   bool
	written, dropped, failed atomic.Uint64
}

const (
	requestRecordTimeout   = 2 * time.Second
	requestRecordBatchSize = 128
	requestRecordBatchWait = 5 * time.Millisecond
)

func NewRequestRecorder(parent context.Context, pool *pgxpool.Pool, log *slog.Logger) *RequestRecorder {
	if log == nil {
		log = slog.Default()
	}
	return startRequestRecorder(parent, New(pool, Config{}), log, 4096)
}

func startRequestRecorder(parent context.Context, store requestRecordStore, log *slog.Logger, capacity int) *RequestRecorder {
	ctx, stop := context.WithCancel(parent)
	s := &RequestRecorder{store: store, log: log, queue: make(chan RequestRecord, capacity), done: make(chan struct{}), stop: stop}
	s.batchStore, _ = store.(requestRecordBatchStore)
	go s.run(ctx)
	return s
}

// RecordRequest copies scalar metadata only; no PostgreSQL, payloads, headers,
// upstream error bodies/messages, target secrets, or blocking logging occur here.
func (s *RequestRecorder) RecordRequest(req *gateway.Request, out gateway.Outcome, settled bool) {
	if req == nil || req.ID == "" || req.Model == "" || req.Principal.UserID <= 0 || req.Principal.KeyID <= 0 {
		return
	}
	r := ProjectRequestRecord(req, out, settled)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		s.dropped.Add(1)
		return
	}
	select {
	case s.queue <- r:
	default:
		s.dropped.Add(1)
	}
}

func (s *RequestRecorder) Close() {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		s.stop()
		<-s.done
	})
}

func (s *RequestRecorder) run(ctx context.Context) {
	defer close(s.done)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var lastDropped, lastFailed uint64
	report := func() {
		dropped, failed := s.dropped.Load(), s.failed.Load()
		if dropped != lastDropped || failed != lastFailed {
			s.log.Warn("request summaries lost", "dropped", dropped-lastDropped, "storage_failures", failed-lastFailed)
			lastDropped, lastFailed = dropped, failed
		}
	}
	defer report()
	for {
		select {
		case <-ctx.Done():
			s.drain(context.WithoutCancel(ctx))
			return
		case r := <-s.queue:
			batch := s.collectBatch(ctx, r, true)
			writeCtx, cancel := context.WithTimeout(ctx, requestRecordTimeout)
			err := s.writeBatch(writeCtx, batch)
			cancel()
			if err == nil {
				s.written.Add(uint64(len(batch)))
			} else if ctx.Err() != nil {
				s.drain(context.WithoutCancel(ctx), batch...)
				return
			} else {
				s.failed.Add(uint64(len(batch)))
			}
		case <-tick.C:
			report()
		}
	}
}

func (s *RequestRecorder) collectBatch(ctx context.Context, first RequestRecord, wait bool) []RequestRecord {
	batch := []RequestRecord{first}
	if s.batchStore == nil {
		return batch
	}
	var timer *time.Timer
	var deadline <-chan time.Time
	if wait {
		timer = time.NewTimer(requestRecordBatchWait)
		deadline = timer.C
		defer timer.Stop()
	}
	for len(batch) < requestRecordBatchSize {
		select {
		case r := <-s.queue:
			batch = append(batch, r)
		default:
			if !wait {
				return batch
			}
			select {
			case r := <-s.queue:
				batch = append(batch, r)
			case <-deadline:
				return batch
			case <-ctx.Done():
				return batch
			}
		}
	}
	return batch
}

func (s *RequestRecorder) writeBatch(ctx context.Context, records []RequestRecord) error {
	if s.batchStore != nil {
		return s.batchStore.RecordRequests(ctx, records)
	}
	return s.store.RecordRequest(ctx, records[0])
}

func (s *RequestRecorder) drain(parent context.Context, pending ...RequestRecord) {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, requestRecordTimeout)
	defer cancel()
	write := func(batch []RequestRecord) {
		if err := s.writeBatch(ctx, batch); err != nil {
			s.failed.Add(uint64(len(batch)))
		} else {
			s.written.Add(uint64(len(batch)))
		}
	}
	if len(pending) > 0 {
		write(pending)
	}
	for {
		if ctx.Err() != nil {
			s.dropped.Add(uint64(len(s.queue)))
			return
		}
		select {
		case r := <-s.queue:
			write(s.collectBatch(ctx, r, false))
		default:
			return
		}
	}
}

func (s *RequestRecorder) Register(reg *prometheus.Registry) {
	for _, item := range []struct {
		name, help string
		count      *atomic.Uint64
	}{
		{"written_total", "Metadata request summaries persisted (including idempotent replay).", &s.written},
		{"dropped_total", "Metadata summaries dropped because the queue was full, closed or shutdown expired.", &s.dropped},
		{"failed_total", "Metadata summaries that failed background storage.", &s.failed},
	} {
		reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Namespace: "codego", Subsystem: "request_audit", Name: item.name, Help: item.help}, func() float64 { return float64(item.count.Load()) }))
	}
	reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Namespace: "codego", Subsystem: "request_audit", Name: "queued", Help: "Metadata summaries awaiting background storage."}, func() float64 { return float64(len(s.queue)) }))
}
