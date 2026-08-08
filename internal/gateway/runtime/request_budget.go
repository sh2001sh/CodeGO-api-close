package runtime

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const requestBudgetContextKey = "gateway_request_budget"

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
		Deadline:        startedAt.Add(requestBudgetDuration(profile.RequestType)),
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

func requestBudgetDuration(requestType RequestType) time.Duration {
	switch requestType {
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
