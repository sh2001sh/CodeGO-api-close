package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type sampleStore interface {
	ShouldSample(string) bool
	RecordSample(context.Context, audit.Sample) error
}

type auditSampler struct {
	store  sampleStore
	log    *slog.Logger
	max    int
	queue  chan audit.Sample
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.RWMutex
	closed bool
	drops  atomic.Uint64
}

const auditDrainTimeout = 2 * time.Second

// newAuditSampler keeps all PostgreSQL work off the request path. Invalid
// explicit configuration fails startup, including when sampling is disabled.
func newAuditSampler(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) (gateway.SampleRecorder, func(), error) {
	rate, err := sampleInt("V3_AUDIT_SAMPLE_RATE_PPM", 0, 0, 1_000_000)
	if err != nil {
		return nil, nil, err
	}
	capacity, err := sampleInt("V3_AUDIT_SAMPLE_QUEUE", 64, 1, 1024)
	if err != nil {
		return nil, nil, err
	}
	maxBytes, err := sampleInt("V3_AUDIT_SAMPLE_MAX_BYTES", 64<<10, 1024, 1<<20)
	if err != nil {
		return nil, nil, err
	}
	if rate == 0 {
		return nil, func() {}, nil
	}
	if pool == nil {
		return nil, nil, fmt.Errorf("audit sampling requires PostgreSQL")
	}
	store := audit.New(pool, audit.Config{SampleRatePPM: rate, MaxSampleBytes: maxBytes})
	s := startAuditSampler(ctx, store, log, capacity, maxBytes)
	return s, s.Close, nil
}

func sampleInt(name string, fallback, low, high int) (int, error) {
	raw, present := os.LookupEnv(name)
	if !present {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < low || n > high {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, low, high)
	}
	return n, nil
}

func startAuditSampler(ctx context.Context, store sampleStore, log *slog.Logger, capacity, maxBytes int) *auditSampler {
	ctx, cancel := context.WithCancel(ctx)
	if log == nil {
		log = slog.Default()
	}
	s := &auditSampler{store: store, log: log, max: maxBytes, queue: make(chan audit.Sample, capacity), cancel: cancel, done: make(chan struct{})}
	go s.run(ctx)
	return s
}

func (s *auditSampler) ShouldSample(id string) bool { return s.store.ShouldSample(id) }
func (s *auditSampler) MaxBytes() int               { return s.max }

// Record snapshots request-owned bytes and never waits for the worker. Payloads
// remain JSON objects so audit.Service can redact all nested credentials before
// writing them to PostgreSQL. Neither payloads nor storage error text are logged.
func (s *auditSampler) Record(req *gateway.Request, out gateway.Outcome, response json.RawMessage) {
	if req == nil || !s.ShouldSample(req.ID) {
		return
	}
	request := json.RawMessage(`{"truncated":true,"reason":"size_limit_or_invalid_json"}`)
	if len(req.Body) <= s.max && json.Valid(req.Body) {
		request = append(json.RawMessage(nil), req.Body...)
	}
	sample := audit.Sample{RequestID: req.ID, UserID: req.Principal.UserID, Model: req.Model,
		CreatedAt: req.Received, Request: request, Response: boundedAuditResponse(response, out, s.max)}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		s.log.Warn("audit sample dropped", "reason", "closed")
		return
	}
	select {
	case s.queue <- sample:
	default:
		s.drops.Add(1)
	}
}

type sampledResponse struct {
	Events    []json.RawMessage `json:"events"`
	Truncated bool              `json:"truncated"`
}

func boundedAuditResponse(raw json.RawMessage, out gateway.Outcome, maxBytes int) json.RawMessage {
	payload := sampledResponse{Events: []json.RawMessage{}, Truncated: true}
	if len(raw) > 0 && len(raw) <= maxBytes {
		if err := json.Unmarshal(raw, &payload); err != nil {
			payload = sampledResponse{Events: []json.RawMessage{}, Truncated: true}
		}
	}
	envelope := struct {
		Terminal       string          `json:"terminal"`
		Delivered      bool            `json:"delivered"`
		Charge         bool            `json:"charge"`
		Usage          gateway.Usage   `json:"usage"`
		UsageTruncated bool            `json:"usage_truncated,omitempty"`
		Response       sampledResponse `json:"response"`
	}{Terminal: out.Terminal.String(), Delivered: out.Delivered, Charge: out.Charge, Usage: out.Usage, Response: payload}
	toolBytes := 0
	for name := range envelope.Usage.ToolCalls {
		toolBytes += len(name) + 32
		if toolBytes > maxBytes/2 {
			envelope.Usage.ToolCalls = nil
			envelope.UsageTruncated = true
			break
		}
	}
	events := envelope.Response.Events
	envelope.Response.Events = []json.RawMessage{}
	base, _ := json.Marshal(envelope)
	if len(base) > maxBytes {
		envelope.Usage.ToolCalls = nil
		envelope.UsageTruncated = true
		base, _ = json.Marshal(envelope)
	}
	size := len(base)
	for i, event := range events {
		added := len(event)
		if i > 0 {
			added++
		}
		if size+added > maxBytes {
			envelope.Response.Truncated = true
			break
		}
		size += added
		envelope.Response.Events = append(envelope.Response.Events, event)
	}
	b, err := json.Marshal(envelope)
	if err == nil && len(b) <= maxBytes {
		return b
	}
	// The validated 1024-byte minimum fits all scalar accounting fields.
	return json.RawMessage(`{"truncated":true,"reason":"metadata_size_limit"}`)
}

func (s *auditSampler) Close() {
	s.cancel()
	<-s.done
}

func (s *auditSampler) run(ctx context.Context) {
	defer close(s.done)
	defer s.reportDrops()
	for {
		select {
		case <-ctx.Done():
			s.drain(nil)
			return
		case sample := <-s.queue:
			writeCtx, cancel := context.WithTimeout(ctx, auditDrainTimeout)
			err := s.store.RecordSample(writeCtx, sample)
			cancel()
			s.reportDrops()
			if ctx.Err() != nil {
				if err != nil {
					s.drain(&sample)
				} else {
					s.drain(nil)
				}
				return
			}
			if err != nil {
				s.log.Warn("audit sample write failed", "reason", "storage_error", "error_type", fmt.Sprintf("%T", err))
			}
		}
	}
}

func (s *auditSampler) reportDrops() {
	if n := s.drops.Swap(0); n > 0 {
		s.log.Warn("audit samples dropped", "reason", "queue_full", "count", n)
	}
}

func (s *auditSampler) drain(pending *audit.Sample) {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), auditDrainTimeout)
	defer cancel()
	if pending != nil {
		s.persistDrain(ctx, *pending)
	}
	for {
		select {
		case <-ctx.Done():
			s.log.Warn("audit samples dropped", "reason", "shutdown_timeout", "queued", len(s.queue))
			return
		case sample := <-s.queue:
			s.persistDrain(ctx, sample)
		default:
			return
		}
	}
}

func (s *auditSampler) persistDrain(ctx context.Context, sample audit.Sample) {
	if err := s.store.RecordSample(ctx, sample); err != nil {
		s.log.Warn("audit sample write failed", "reason", "shutdown_storage_error", "error_type", fmt.Sprintf("%T", err))
	}
}
