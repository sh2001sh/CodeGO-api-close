package runtime

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/types"
)

const requestBudgetContextKey = "gateway_request_budget"

const (
	responsesStreamRetryBudget      = 150 * time.Second
	largeRequestBodyBudgetThreshold = 8 << 20
)

type RequestBudget struct {
	StartedAt        time.Time `json:"started_at"`
	Deadline         time.Time `json:"deadline"`
	MaxAttempts      int       `json:"max_attempts"`
	AttemptsUsed     int       `json:"attempts_used"`
	MaxFaultDomains  int       `json:"max_fault_domains"`
	FaultDomainsUsed int       `json:"fault_domains_used"`

	mu           sync.Mutex
	faultDomains map[string]struct{}
}

// StartRequestBudget creates the request-level retry budget once. Repeated
// calls return the existing budget so retries never reset the deadline.
func StartRequestBudget(c *gin.Context, profile RequestProfile, startedAt time.Time) *RequestBudget {
	if existing := RequestBudgetFromContext(c); existing != nil {
		return existing
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	budget := &RequestBudget{
		StartedAt:       startedAt,
		Deadline:        startedAt.Add(requestBudgetDuration(profile)),
		MaxAttempts:     2,
		MaxFaultDomains: 2,
		faultDomains:    make(map[string]struct{}, 2),
	}
	if c != nil {
		c.Set(requestBudgetContextKey, budget)
		UpdateRouteDecisionBudget(c, budget)
	}
	return budget
}

// RequestBudgetStartTime keeps slow client uploads from consuming the retry
// budget reserved for upstream processing. Only genuinely large bodies use
// the post-validation start; small requests retain the original end-to-end
// deadline semantics.
func RequestBudgetStartTime(requestStartedAt, validatedAt time.Time, bodySize int64) time.Time {
	if bodySize >= largeRequestBodyBudgetThreshold && !validatedAt.IsZero() {
		return validatedAt
	}
	return requestStartedAt
}

// AutomaticRouteFirstByteTimeout intentionally disables speculative failover
// after an Auto request has been dispatched. The upstream may continue
// billable work after the local connection is cancelled.
func AutomaticRouteFirstByteTimeout(c *gin.Context) time.Duration {
	// Once the request body has been written, a provider may keep generating and
	// charge for the request even if cancelling the HTTP context succeeds
	// locally. A speculative first-byte deadline therefore cannot safely trigger
	// a full-request replay through another paid route. Explicit transport and
	// upstream HTTP failures still use the bounded request retry budget.
	return 0
}

const automaticFirstByteDeadlineKey = "automatic_first_byte_deadline"

// StartAutomaticFirstByteWait starts one clock for both response headers and
// semantic output. Each upstream attempt replaces the previous deadline.
func StartAutomaticFirstByteWait(c *gin.Context) time.Duration {
	wait := AutomaticRouteFirstByteTimeout(c)
	var deadline time.Time
	if wait > 0 {
		if budget := RequestBudgetFromContext(c); budget != nil {
			wait = min(wait, budget.Remaining(time.Now()))
		}
		deadline = time.Now().Add(wait)
	}
	if c != nil {
		c.Set(automaticFirstByteDeadlineKey, deadline)
	}
	updateRouteDecision(c, func(d *RouteDecision) {
		d.FirstByteTimeoutMS = wait.Milliseconds()
		if len(d.Attempts) > 0 {
			d.Attempts[len(d.Attempts)-1].FirstByteTimeoutMS = wait.Milliseconds()
		}
		if profile, found := RequestProfileFromContext(c); found {
			d.MigrationCapability = profile.MigrationCapability
		}
	})
	return wait
}

func RemainingAutomaticFirstByteWait(c *gin.Context) time.Duration {
	if c != nil {
		if value, exists := c.Get(automaticFirstByteDeadlineKey); exists {
			if deadline, ok := value.(time.Time); ok && !deadline.IsZero() {
				return max(time.Nanosecond, time.Until(deadline))
			}
			return 0
		}
	}
	return AutomaticRouteFirstByteTimeout(c)
}

func RequestBudgetFromContext(c *gin.Context) *RequestBudget {
	if c == nil {
		return nil
	}
	value, found := c.Get(requestBudgetContextKey)
	if !found {
		return nil
	}
	budget, _ := value.(*RequestBudget)
	return budget
}

// ExpandRequestBudget allows a configured Auto pool to visit its remaining
// groups while preserving the original request deadline.
func ExpandRequestBudget(budget *RequestBudget, attempts int) {
	if budget == nil || attempts <= 0 {
		return
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if attempts > budget.MaxAttempts {
		budget.MaxAttempts = attempts
	}
	if attempts > budget.MaxFaultDomains {
		budget.MaxFaultDomains = attempts
	}
}

func (b *RequestBudget) TryBeginAttempt(now time.Time, faultDomain string) bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if now.IsZero() {
		now = time.Now()
	}
	if b.AttemptsUsed >= b.MaxAttempts || !now.Before(b.Deadline) {
		return false
	}
	faultDomain = normalizeFaultDomain(faultDomain)
	if faultDomain != "" {
		if _, found := b.faultDomains[faultDomain]; !found {
			if len(b.faultDomains) >= b.MaxFaultDomains {
				return false
			}
			b.faultDomains[faultDomain] = struct{}{}
			b.FaultDomainsUsed = len(b.faultDomains)
		}
	}
	b.AttemptsUsed++
	return true
}

func (b *RequestBudget) CanRetry(now time.Time) bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if now.IsZero() {
		now = time.Now()
	}
	return b.AttemptsUsed < b.MaxAttempts && now.Before(b.Deadline)
}

func (b *RequestBudget) Remaining(now time.Time) time.Duration {
	if b == nil {
		return 0
	}
	if now.IsZero() {
		now = time.Now()
	}
	remaining := b.Deadline.Sub(now)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func requestBudgetDuration(profile RequestProfile) time.Duration {
	if profile.IsStream && profile.Protocol == string(types.RelayFormatOpenAIResponses) && profile.RequestType != RequestTypeImageStream {
		return responsesStreamRetryBudget
	}
	switch profile.RequestType {
	case RequestTypeChatShortStream:
		return 35 * time.Second
	case RequestTypeChatLongStream:
		return 90 * time.Second
	case RequestTypeToolCallStream:
		return 60 * time.Second
	case RequestTypeImageNonStream, RequestTypeImageStream:
		return 180 * time.Second
	default:
		return 60 * time.Second
	}
}
