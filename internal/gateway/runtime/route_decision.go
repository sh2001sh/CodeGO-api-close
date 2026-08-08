package runtime

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
)

const routeDecisionContextKey = "route_decision_audit"

const (
	RouteDecisionProbeNormal     = "normal"
	RouteDecisionProbeLastResort = "last_resort"
	RouteDecisionProbeRateLimit  = "rate_limit_retry"
	RouteDecisionProbeEmergency  = "emergency_retry"
)

// RouteDecision is an internal-only record attached to the existing request audit log.
// It contains identifiers, not provider credentials or channel names.
type RouteDecision struct {
	RequestID       string         `json:"request_id"`
	Model           string         `json:"model"`
	RequestedGroup  string         `json:"requested_group"`
	SelectedGroup   string         `json:"selected_group,omitempty"`
	Mode            string         `json:"mode,omitempty"`
	ChannelID       int            `json:"channel_id,omitempty"`
	CandidateGroups int            `json:"candidate_groups"`
	Excluded        []string       `json:"excluded,omitempty"`
	RetryCount      int            `json:"retry_count"`
	AffinityHit     bool           `json:"affinity_hit"`
	Fallback        bool           `json:"fallback"`
	HealthState     string         `json:"health_state,omitempty"`
	ProbeMode       string         `json:"probe_mode,omitempty"`
	RequestType     RequestType    `json:"request_type,omitempty"`
	Protocol        string         `json:"protocol,omitempty"`
	AttemptsUsed    int            `json:"attempts_used,omitempty"`
	MaxAttempts     int            `json:"max_attempts,omitempty"`
	BudgetRemaining int64          `json:"budget_remaining_ms,omitempty"`
	Attempts        []RouteAttempt `json:"attempts,omitempty"`
}

type RouteAttempt struct {
	AttemptID    string      `json:"attempt_id"`
	RetryIndex   int         `json:"retry_index"`
	ChannelID    int         `json:"channel_id"`
	FaultDomain  string      `json:"fault_domain,omitempty"`
	RequestType  RequestType `json:"request_type"`
	StartedAt    time.Time   `json:"started_at"`
	DurationMS   int64       `json:"duration_ms,omitempty"`
	Stage        string      `json:"stage,omitempty"`
	StatusCode   int         `json:"status_code,omitempty"`
	FailureClass string      `json:"failure_class,omitempty"`
	Success      bool        `json:"success"`
}

// MarkAutomaticPool records that the selected channel came from the cost and
// health based automatic pool rather than legacy priority/weight selection.
func MarkAutomaticPool(c *gin.Context) {
	updateRouteDecision(c, func(decision *RouteDecision) {
		decision.Mode = "automatic_pool"
	})
}

func StartRouteDecision(c *gin.Context, model string, requestedGroup string) {
	if c == nil {
		return
	}
	decision := RouteDecision{
		RequestID:      c.GetString(constant.RequestIdKey),
		Model:          strings.TrimSpace(model),
		RequestedGroup: strings.TrimSpace(requestedGroup),
	}
	if profile, found := RequestProfileFromContext(c); found {
		decision.RequestType = profile.RequestType
		decision.Protocol = profile.Protocol
	}
	c.Set(routeDecisionContextKey, decision)
}

func AttachRouteDecisionProfile(c *gin.Context, profile RequestProfile) {
	updateRouteDecision(c, func(decision *RouteDecision) {
		decision.RequestType = profile.RequestType
		decision.Protocol = profile.Protocol
	})
}

func UpdateRouteDecisionBudget(c *gin.Context, budget *RequestBudget) {
	if budget == nil {
		return
	}
	updateRouteDecision(c, func(decision *RouteDecision) {
		decision.AttemptsUsed = budget.AttemptsUsed
		decision.MaxAttempts = budget.MaxAttempts
		decision.BudgetRemaining = budget.Remaining(time.Now()).Milliseconds()
	})
}

func StartRouteDecisionAttempt(c *gin.Context, retryIndex, channelID int, faultDomain string) {
	updateRouteDecision(c, func(decision *RouteDecision) {
		attemptNumber := len(decision.Attempts) + 1
		decision.Attempts = append(decision.Attempts, RouteAttempt{
			AttemptID:   decision.RequestID + ":" + strconv.Itoa(attemptNumber),
			RetryIndex:  retryIndex,
			ChannelID:   channelID,
			FaultDomain: strings.TrimSpace(faultDomain),
			RequestType: RequestTypeFromContext(c),
			StartedAt:   time.Now().UTC(),
		})
	})
}

func FinishRouteDecisionAttempt(c *gin.Context, success bool, statusCode int, failureClass, stage string) {
	updateRouteDecision(c, func(decision *RouteDecision) {
		if len(decision.Attempts) == 0 {
			return
		}
		attempt := &decision.Attempts[len(decision.Attempts)-1]
		attempt.Success = success
		attempt.StatusCode = statusCode
		attempt.FailureClass = strings.TrimSpace(failureClass)
		attempt.Stage = strings.TrimSpace(stage)
		attempt.DurationMS = time.Since(attempt.StartedAt).Milliseconds()
	})
}

func UpdateRouteDecisionCandidates(c *gin.Context, count int) {
	updateRouteDecision(c, func(decision *RouteDecision) {
		if count > decision.CandidateGroups {
			decision.CandidateGroups = count
		}
	})
}

func ExcludeRouteDecisionCandidate(c *gin.Context, reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	updateRouteDecision(c, func(decision *RouteDecision) {
		for _, existing := range decision.Excluded {
			if existing == reason {
				return
			}
		}
		decision.Excluded = append(decision.Excluded, reason)
	})
}

func SelectRouteDecisionCandidate(c *gin.Context, group string, channelID int, affinityHit bool) {
	updateRouteDecision(c, func(decision *RouteDecision) {
		if decision.SelectedGroup != "" && decision.SelectedGroup != group {
			decision.Fallback = true
		}
		decision.SelectedGroup = strings.TrimSpace(group)
		decision.ChannelID = channelID
		decision.AffinityHit = affinityHit
		if health, found := GetChannelHealth(channelID, decision.Model, RequestTypeFromContext(c)); found {
			decision.HealthState = health.State
		} else {
			decision.HealthState = ChannelHealthHealthy
		}
	})
}

func RecordRouteDecisionRetry(c *gin.Context) {
	updateRouteDecision(c, func(decision *RouteDecision) {
		decision.RetryCount++
		decision.Fallback = true
	})
}

// SetRouteDecisionProbeMode records the recovery probe path used for this
// request. It is emitted only in administrator audit metadata.
func SetRouteDecisionProbeMode(c *gin.Context, mode string) {
	switch mode {
	case RouteDecisionProbeNormal, RouteDecisionProbeLastResort, RouteDecisionProbeRateLimit, RouteDecisionProbeEmergency:
		updateRouteDecision(c, func(decision *RouteDecision) {
			decision.ProbeMode = mode
		})
	}
}

// GetRouteDecision returns a copy suitable for administrators' log metadata.
func GetRouteDecision(c *gin.Context) (RouteDecision, bool) {
	if c == nil {
		return RouteDecision{}, false
	}
	value, ok := c.Get(routeDecisionContextKey)
	if !ok {
		return RouteDecision{}, false
	}
	decision, ok := value.(RouteDecision)
	return decision, ok
}

func updateRouteDecision(c *gin.Context, update func(*RouteDecision)) {
	if c == nil {
		return
	}
	value, ok := c.Get(routeDecisionContextKey)
	if !ok {
		return
	}
	decision, ok := value.(RouteDecision)
	if !ok {
		return
	}
	update(&decision)
	c.Set(routeDecisionContextKey, decision)
}
