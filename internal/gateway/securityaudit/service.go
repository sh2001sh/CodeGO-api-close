package securityaudit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

type MetricsSnapshot struct {
	Total       int64
	Allowed     int64
	Flagged     int64
	Blocked     int64
	Unavailable int64
	Invalid     int64
	Dropped     int64
}

type auditMetrics struct {
	total       atomic.Int64
	allowed     atomic.Int64
	flagged     atomic.Int64
	blocked     atomic.Int64
	unavailable atomic.Int64
	invalid     atomic.Int64
	dropped     atomic.Int64
}

type Service struct {
	config  Config
	scanner Scanner
	queue   chan Request
	metrics auditMetrics

	global chan struct{}
	nodeMu sync.Mutex
	nodes  map[string]chan struct{}
}

var (
	defaultServiceOnce sync.Once
	defaultService     *Service
)

func buildDefaultService() *Service {
	config, err := ConfigFromEnv()
	if err != nil {
		requestedMode := normalizeModeFromEnvironment()
		platformobservability.SysError("prompt audit configuration invalid: " + err.Error())
		config = Config{Mode: requestedMode}
		if requestedMode != ModeBlocking {
			config.Mode = ModeOff
		}
	}
	return NewService(config, NewQwenScanner())
}

func DefaultService() *Service {
	defaultServiceOnce.Do(func() {
		defaultService = buildDefaultService()
	})
	return defaultService
}

func NewService(config Config, scanner Scanner) *Service {
	normalizeConfig(&config)
	service := &Service{
		config: config, scanner: scanner,
		global: make(chan struct{}, config.GlobalConcurrency), nodes: make(map[string]chan struct{}),
	}
	if config.Mode == ModeAsync {
		service.queue = make(chan Request, config.QueueCapacity)
		for range config.WorkerCount {
			go service.runWorker()
		}
	}
	return service
}

func (s *Service) Mode() Mode {
	if s == nil {
		return ModeOff
	}
	return s.config.Mode
}

// IsBlockingForGroup reports whether synchronous enforcement applies to a group.
func (s *Service) IsBlockingForGroup(group string) bool {
	return s != nil && s.config.Mode == ModeBlocking && s.config.includesGroup(group)
}

func (s *Service) Check(ctx context.Context, request Request) Decision {
	if s == nil || s.config.Mode == ModeOff || !s.config.includesGroup(request.Group) {
		return allowDecision(nil)
	}
	if s.config.Mode == ModeAsync {
		select {
		case s.queue <- request.clone():
		default:
			s.metrics.dropped.Add(1)
			platformobservability.SysError("prompt audit async queue full; request_id=" + request.RequestID)
		}
		return allowDecision(nil)
	}
	snapshot, err := ExtractSnapshot(request, s.config.LatestTurnOnly)
	if errors.Is(err, errNoPromptText) {
		return allowDecision(nil)
	}
	if err != nil {
		s.metrics.invalid.Add(1)
		return Decision{Kind: DecisionInvalid, ErrorCode: ErrorCodeInvalidResponse}
	}
	return s.evaluate(ctx, snapshot)
}

func (s *Service) Metrics() MetricsSnapshot {
	if s == nil {
		return MetricsSnapshot{}
	}
	return MetricsSnapshot{
		Total: s.metrics.total.Load(), Allowed: s.metrics.allowed.Load(), Flagged: s.metrics.flagged.Load(),
		Blocked: s.metrics.blocked.Load(), Unavailable: s.metrics.unavailable.Load(), Invalid: s.metrics.invalid.Load(),
		Dropped: s.metrics.dropped.Load(),
	}
}

func (s *Service) runWorker() {
	for request := range s.queue {
		snapshot, err := ExtractSnapshot(request, s.config.LatestTurnOnly)
		if errors.Is(err, errNoPromptText) {
			continue
		}
		if err != nil {
			s.metrics.invalid.Add(1)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), s.auditTimeout())
		decision := s.evaluate(ctx, snapshot)
		cancel()
		if decision.Kind == DecisionBlock || decision.Kind == DecisionFlag {
			platformobservability.SysLog(formatAuditLog(snapshot, decision))
		}
	}
}

func (s *Service) evaluate(ctx context.Context, snapshot Snapshot) Decision {
	s.metrics.total.Add(1)
	if s.scanner == nil || len(s.config.Endpoints) == 0 {
		s.metrics.unavailable.Add(1)
		return Decision{Kind: DecisionUnavailable, ErrorCode: ErrorCodeUnavailable}
	}
	select {
	case s.global <- struct{}{}:
		defer func() { <-s.global }()
	default:
		s.metrics.unavailable.Add(1)
		return Decision{Kind: DecisionUnavailable, ErrorCode: ErrorCodeUnavailable}
	}
	evalCtx, cancel := context.WithTimeout(ctx, s.auditTimeout())
	defer cancel()
	chunks := splitRunes(snapshot.ScanText, s.minimumInputLimit())
	combined := &GuardResult{Action: "allow", Chunks: len(chunks)}
	startedAt := time.Now()
	for _, chunk := range chunks {
		result, err := s.scanChunk(evalCtx, chunk)
		if err != nil {
			code := guardErrorCode(err)
			if code == ErrorCodeInvalidResponse {
				s.metrics.invalid.Add(1)
				return Decision{Kind: DecisionInvalid, ErrorCode: code}
			}
			s.metrics.unavailable.Add(1)
			return Decision{Kind: DecisionUnavailable, ErrorCode: code}
		}
		result = s.applyBlockingPolicy(result)
		mergeResult(combined, result)
		if result.Action == "block" {
			break
		}
	}
	combined.Latency = time.Since(startedAt)
	switch combined.Action {
	case "block":
		s.metrics.blocked.Add(1)
		decision := Decision{Kind: DecisionBlock, ErrorCode: ErrorCodeBlocked, Result: combined}
		platformobservability.SysLog(formatAuditLog(snapshot, decision))
		return decision
	case "warn":
		s.metrics.flagged.Add(1)
		return Decision{Kind: DecisionFlag, AllowNextStage: true, Result: combined}
	default:
		s.metrics.allowed.Add(1)
		return allowDecision(combined)
	}
}

func (s *Service) applyBlockingPolicy(result *GuardResult) *GuardResult {
	if result == nil || !s.config.BlockControversial || result.Safety != "Controversial" {
		return result
	}
	if !containsElevatedCategory(result.MatchedScanners) {
		return result
	}
	policyResult := *result
	policyResult.Action = "block"
	return &policyResult
}

func (s *Service) scanChunk(ctx context.Context, chunk string) (*GuardResult, error) {
	var lastErr error
	for _, endpoint := range s.config.Endpoints {
		semaphore := s.nodeSemaphore(endpoint.ID)
		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: true, Cause: ctx.Err()}
		default:
			lastErr = &GuardError{Code: ErrorCodeUnavailable, Retryable: true}
			continue
		}
		result, err := s.scanner.Scan(ctx, endpoint, chunk, s.config.Scanners)
		<-semaphore
		if err == nil && result != nil {
			return result, nil
		}
		if err == nil {
			err = &GuardError{Code: ErrorCodeInvalidResponse}
		}
		lastErr = err
		var guardErr *GuardError
		if !errors.As(err, &guardErr) || !guardErr.Retryable {
			return nil, err
		}
	}
	if lastErr == nil {
		lastErr = &GuardError{Code: ErrorCodeUnavailable}
	}
	return nil, lastErr
}

func (s *Service) nodeSemaphore(endpointID string) chan struct{} {
	s.nodeMu.Lock()
	defer s.nodeMu.Unlock()
	semaphore := s.nodes[endpointID]
	if semaphore == nil {
		semaphore = make(chan struct{}, s.config.PerNodeConcurrency)
		s.nodes[endpointID] = semaphore
	}
	return semaphore
}

func (s *Service) auditTimeout() time.Duration {
	if len(s.config.Endpoints) == 0 {
		return defaultTimeout
	}
	return s.config.Endpoints[0].timeout()
}

func (s *Service) minimumInputLimit() int {
	limit := defaultInputLimit
	for index, endpoint := range s.config.Endpoints {
		if index == 0 || endpoint.inputLimit() < limit {
			limit = endpoint.inputLimit()
		}
	}
	return limit
}
