package app

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	requestsettings "github.com/sh2001sh/new-api/internal/platform/requestsettings"
)

const (
	promptSafetyRiskWindow      = 24 * time.Hour
	promptSafetyCacheTTL        = time.Minute
	promptSafetyReviewThreshold = 6
	promptSafetyBlockThreshold  = 12
	promptSafetyMaxScanBytes    = 64 * 1024
)

type promptSafetyRule struct {
	label    string
	phrases  []string
	riskCost int
}

type promptSafetyCacheEntry struct {
	score     int
	expiresAt time.Time
}

// PromptSafetyDecision exposes category-level results without retaining prompt content.
type PromptSafetyDecision struct {
	LocalRisk int
	RiskScore int
	Strict    bool
	Block     bool
	Labels    []string
}

var promptSafetyRules = []promptSafetyRule{
	{label: "instruction_override", phrases: []string{"ignore previous instructions", "ignore all previous instructions", "disregard previous instructions", "忽略之前的指令", "忽略以上指令", "无视之前的规则"}, riskCost: 3},
	{label: "policy_evasion", phrases: []string{"jailbreak", "developer mode", "dan mode", "bypass safety", "bypass policy", "绕过安全限制", "绕过内容审核", "解除安全限制", "越狱"}, riskCost: 4},
	{label: "system_impersonation", phrases: []string{"system message override", "you are now system", "pretend to be the system", "伪造系统消息", "你现在是系统管理员"}, riskCost: 2},
}

var promptSafetyRiskCache = struct {
	sync.Mutex
	entries map[int]promptSafetyCacheEntry
}{entries: make(map[int]promptSafetyCacheEntry)}

// EvaluatePromptSafety runs a bounded local prefilter and raises a rolling user risk score.
// It stores only numeric risk and category labels, never the prompt itself.
func EvaluatePromptSafety(userID int, text string) PromptSafetyDecision {
	if !requestsettings.ShouldRunPromptSafety() || strings.TrimSpace(text) == "" {
		return PromptSafetyDecision{}
	}

	normalized := strings.ToLower(limitPromptSafetyText(text))
	decision := PromptSafetyDecision{}
	for _, rule := range promptSafetyRules {
		if containsAnyPromptSafetyPhrase(normalized, rule.phrases) {
			decision.LocalRisk += rule.riskCost
			decision.Labels = append(decision.Labels, rule.label)
		}
	}
	if userID <= 0 {
		decision.Strict = decision.LocalRisk >= promptSafetyReviewThreshold
		decision.Block = decision.LocalRisk >= promptSafetyBlockThreshold
		return decision
	}

	if decision.LocalRisk == 0 {
		decision.RiskScore, _ = cachedPromptSafetyRisk(userID, time.Now())
		decision.Strict = decision.RiskScore >= promptSafetyReviewThreshold
		return decision
	}
	decision.RiskScore = addPromptSafetyRisk(userID, decision.LocalRisk)
	decision.Strict = decision.LocalRisk >= promptSafetyReviewThreshold || decision.RiskScore >= promptSafetyReviewThreshold
	decision.Block = decision.RiskScore >= promptSafetyBlockThreshold && decision.LocalRisk > 0
	return decision
}

func containsAnyPromptSafetyPhrase(normalized string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}
	return false
}

func limitPromptSafetyText(text string) string {
	if len(text) <= promptSafetyMaxScanBytes {
		return text
	}
	half := promptSafetyMaxScanBytes / 2
	return text[:half] + text[len(text)-half:]
}

func promptSafetyRiskScore(userID int) int {
	now := time.Now()
	if score, found := cachedPromptSafetyRisk(userID, now); found {
		return score
	}

	score := loadPromptSafetyRisk(userID)
	if score > 0 {
		cachePromptSafetyRisk(userID, score, now.Add(promptSafetyCacheTTL))
	}
	return score
}

func cachedPromptSafetyRisk(userID int, now time.Time) (int, bool) {
	promptSafetyRiskCache.Lock()
	defer promptSafetyRiskCache.Unlock()
	entry, found := promptSafetyRiskCache.entries[userID]
	if found && entry.expiresAt.After(now) {
		return entry.score, true
	}
	if found {
		delete(promptSafetyRiskCache.entries, userID)
	}
	return 0, false
}

func addPromptSafetyRisk(userID, delta int) int {
	now := time.Now()
	score := promptSafetyRiskScore(userID) + delta
	if platformcache.RedisReady() {
		key := promptSafetyRiskKey(userID)
		value, err := platformcache.RDB.IncrBy(context.Background(), key, int64(delta)).Result()
		if err == nil {
			score = int(value)
			if value == int64(delta) {
				_ = platformcache.RDB.Expire(context.Background(), key, promptSafetyRiskWindow).Err()
			}
		}
	}
	cachePromptSafetyRisk(userID, score, now.Add(promptSafetyCacheTTL))
	return score
}

func loadPromptSafetyRisk(userID int) int {
	if !platformcache.RedisReady() {
		return 0
	}
	value, err := platformcache.RDB.Get(context.Background(), promptSafetyRiskKey(userID)).Result()
	if err != nil {
		return 0
	}
	score, err := strconv.Atoi(value)
	if err != nil || score < 0 {
		return 0
	}
	return score
}

func cachePromptSafetyRisk(userID, score int, expiresAt time.Time) {
	promptSafetyRiskCache.Lock()
	defer promptSafetyRiskCache.Unlock()
	if score <= 0 {
		delete(promptSafetyRiskCache.entries, userID)
		return
	}
	promptSafetyRiskCache.entries[userID] = promptSafetyCacheEntry{score: score, expiresAt: expiresAt}
}

func promptSafetyRiskKey(userID int) string {
	return "prompt-safety-risk:" + strconv.Itoa(userID)
}
