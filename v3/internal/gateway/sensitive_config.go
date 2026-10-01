package gateway

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

type sensitiveRule struct {
	substring string
	regex     *regexp.Regexp
}

type sensitiveConfiguration struct {
	enabled, onPrompt, stop bool
	rules                   []sensitiveRule
}

// Defaults intentionally match v2 requestsettings rather than introducing an
// independent list or changing the historical opt-out behavior.
var defaultSensitiveWords = []string{
	"contains:credential stuffing", "contains:steal browser cookies",
	"contains:sql injection payload", "contains:bypass rate limit",
	"contains:bypass moderation", "contains:evade safety filter",
	"contains:reverse shell", "contains:privilege escalation",
	"contains:keylogger", "contains:ransomware", "contains:malware loader",
	"contains:crack password", "contains:phishing page",
}

func loadSensitiveConfiguration(values map[string]json.RawMessage) (*sensitiveConfiguration, error) {
	configuration := &sensitiveConfiguration{}
	var err error
	configuration.enabled, err = sensitiveSettingBool(values, "CheckSensitiveEnabled")
	if err != nil {
		return nil, err
	}
	configuration.onPrompt, err = sensitiveSettingBool(values, "CheckSensitiveOnPromptEnabled")
	if err != nil {
		return nil, err
	}
	configuration.stop, err = sensitiveSettingBool(values, "StopOnSensitiveEnabled")
	if err != nil {
		return nil, err
	}
	words, err := sensitiveWordsSetting(values)
	if err != nil {
		return nil, err
	}
	configuration.rules, err = parseSensitiveRules(words)
	if err != nil {
		return nil, err
	}
	return configuration, nil
}

// sensitiveWordsSetting resolves the configured word list, falling back to
// defaultSensitiveWords when "SensitiveWords" is absent.
func sensitiveWordsSetting(values map[string]json.RawMessage) ([]string, error) {
	raw, present := values["SensitiveWords"]
	if !present {
		return defaultSensitiveWords, nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil, errors.New("invalid sensitive words configuration")
	}
	switch configured := value.(type) {
	case string:
		return strings.Split(configured, "\n"), nil
	case []any:
		words := make([]string, 0, len(configured))
		for _, word := range configured {
			text, ok := word.(string)
			if !ok {
				return nil, errors.New("sensitive rules must be strings")
			}
			words = append(words, text)
		}
		return words, nil
	default:
		return nil, errors.New("sensitive words must be text or string array")
	}
}

// parseSensitiveRules compiles each raw word/pattern into a sensitiveRule,
// failing closed on malformed regexes rather than silently ignoring them.
func parseSensitiveRules(words []string) ([]sensitiveRule, error) {
	var rules []sensitiveRule
	for _, raw := range words {
		rule := strings.TrimSpace(raw)
		if rule == "" {
			continue
		}
		entry := sensitiveRule{}
		switch {
		case strings.HasPrefix(rule, "re:"):
			pattern := strings.TrimSpace(strings.TrimPrefix(rule, "re:"))
			if pattern == "" {
				return nil, errors.New("empty sensitive regular expression")
			}
			var err error
			entry.regex, err = regexp.Compile(pattern)
			// Unlike v2's ignored malformed regex, invalid policy is explicit and
			// fails closed. Never include patterns in client errors or log fields.
			if err != nil {
				return nil, errors.New("invalid sensitive regular expression")
			}
		case strings.HasPrefix(rule, "contains:"):
			entry.substring = strings.ToLower(strings.TrimPrefix(rule, "contains:"))
		default:
			entry.substring = strings.ToLower(rule)
		}
		rules = append(rules, entry)
	}
	return rules, nil
}

func sensitiveSettingBool(values map[string]json.RawMessage, name string) (bool, error) {
	raw, present := values[name]
	if !present {
		return true, nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false, errors.New("invalid sensitive boolean setting")
	}
	switch boolean := value.(type) {
	case bool:
		return boolean, nil
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(boolean))
		if err != nil {
			return false, errors.New("invalid sensitive boolean setting")
		}
		return parsed, nil
	default:
		return false, errors.New("invalid sensitive boolean setting")
	}
}
