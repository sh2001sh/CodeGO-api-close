package app

import (
	requestsettings "github.com/sh2001sh/new-api/internal/platform/requestsettings"
	"regexp"
	"strings"
	"sync"
)

var (
	identityRegexMu    sync.Mutex
	identityRegexCache = map[string]*regexp.Regexp{}
)

func CheckSensitiveText(text string) (bool, []string) {
	return SensitiveWordContains(text)
}

func ShouldReviewPromptWithGuard(text string) (bool, []string) {
	return MatchConfiguredPatterns(text, requestsettings.PromptAuditReviewRules)
}

func SensitiveWordContains(text string) (bool, []string) {
	return MatchConfiguredPatterns(text, requestsettings.SensitiveWords)
}

func MatchConfiguredPatterns(text string, rules []string) (bool, []string) {
	if len(rules) == 0 || len(text) == 0 {
		return false, nil
	}
	plain := make([]string, 0, len(rules))
	hits := make([]string, 0, 2)
	checkText := lowerString(text)
	for _, rawRule := range rules {
		rule := strings.TrimSpace(rawRule)
		if rule == "" {
			continue
		}
		switch {
		case strings.HasPrefix(rule, "re:"):
			pattern := strings.TrimSpace(strings.TrimPrefix(rule, "re:"))
			if pattern == "" {
				continue
			}
			re, ok := getOrBuildIdentityRegex(pattern)
			if ok && re.MatchString(text) {
				return true, []string{rule}
			}
		case strings.HasPrefix(rule, "contains:"):
			needle := lowerString(strings.TrimPrefix(rule, "contains:"))
			if needle != "" && strings.Contains(checkText, needle) {
				return true, []string{rule}
			}
		default:
			plain = append(plain, rule)
		}
	}
	if len(plain) == 0 {
		return false, nil
	}
	contains, words := AcSearchLower(text, plain)
	if !contains {
		return false, nil
	}
	for _, word := range words {
		hits = append(hits, word)
	}
	return true, hits
}

func getOrBuildIdentityRegex(pattern string) (*regexp.Regexp, bool) {
	identityRegexMu.Lock()
	defer identityRegexMu.Unlock()
	if re, ok := identityRegexCache[pattern]; ok {
		return re, re != nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		identityRegexCache[pattern] = nil
		return nil, false
	}
	identityRegexCache[pattern] = re
	return re, true
}

func AcSearchLower(text string, words []string) (bool, []string) {
	checkText := []rune(lowerString(text))
	hits := getOrBuildIdentityAC(words).MultiPatternSearch(checkText, true)
	if len(hits) == 0 {
		return false, nil
	}
	result := make([]string, 0, len(hits))
	for _, hit := range hits {
		result = append(result, string(hit.Word))
	}
	return true, result
}
