package runtime

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/types"
)

const requestBudgetContextKey = "gateway_request_budget"

const (
	responsesStreamRetryBudget       = 150 * time.Second
	responsesFirstAttemptWaitTimeout = 60 * time.Second
	responsesShortAttemptDefault     = 30 * time.Second
	responsesShortAttemptMin         = 15 * time.Second
	responsesShortAttemptMax         = 30 * time.Second
	responsesAdaptiveTTFTMinSamples  = 10
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

// RetryableResponsesAttemptTimeout returns the first-attempt wait cap only
// while a text Responses stream still has a real retry available.
func RetryableResponsesAttemptTimeout(c *gin.Context) time.Duration {
	if c == nil || IsImageGenerationRequest(c) {
		return 0
	}
	if IsSingleChannelRoute(c) && !HasRemainingCrossGroupRoute(c) {
		return 0
	}
	if _, specificChannel := c.Get("specific_channel_id"); specificChannel {
		return 0
	}
	profile, found := RequestProfileFromContext(c)
	if !found || !profile.IsStream || profile.Protocol != string(types.RelayFormatOpenAIResponses) {
		return 0
	}
	budget := RequestBudgetFromContext(c)
	if budget == nil || !budget.CanRetry(time.Now()) {
		return 0
	}
	if profile.RequestType == RequestTypeChatLongStream || profile.RequestType == RequestTypeToolCallStream {
		return responsesFirstAttemptWaitTimeout
	}
	channelID := c.GetInt(string(constant.ContextKeyChannelId))
	model := c.GetString(string(constant.ContextKeyOriginalModel))
	health, found := GetChannelHealth(channelID, model, profile.RequestType)
	if !found || health.TTFTSamples < responsesAdaptiveTTFTMinSamples || health.TTFTP95Milliseconds <= 0 {
		return responsesShortAttemptDefault
	}
	timeout := time.Duration(health.TTFTP95Milliseconds*1.25) * time.Millisecond
	if timeout < responsesShortAttemptMin {
		return responsesShortAttemptMin
	}
	if timeout > responsesShortAttemptMax {
		return responsesShortAttemptMax
	}
	return timeout
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
