package gateway

import (
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
)

type sensitivePolicyState struct {
	mu      sync.Mutex
	loaded  bool
	key     [32]byte
	current *sensitiveConfiguration
	err     error
}

// NewSensitiveWordPolicy reads immutable in-process settings snapshots. Only
// the current configuration is retained; request input never becomes a cache key.
func NewSensitiveWordPolicy(settings func() map[string]json.RawMessage, log *slog.Logger) TargetPolicy {
	if log == nil {
		log = slog.Default()
	}
	state := &sensitivePolicyState{}
	return func(req *Request, target Target) error {
		if req == nil {
			return sensitivePolicyUnavailable()
		}
		// v2 deliberately exempts the caller's Claude protocol, independent of
		// which upstream adapter or protocol bridge will later be selected.
		if req.Protocol == ProtocolAnthropic {
			return nil
		}
		enabled, err := sensitiveWordInterceptionEnabled(target)
		if err != nil {
			return err
		}
		if !enabled {
			return nil
		}
		values := map[string]json.RawMessage{}
		if settings != nil {
			values = settings()
		}
		configuration, err := state.configuration(values)
		if err != nil {
			return sensitivePolicyUnavailable()
		}
		if !configuration.enabled || !configuration.onPrompt {
			return nil
		}
		text := sensitivePromptText(req)
		if text == "" {
			return nil
		}
		hits := countSensitiveWordHits(text, configuration.rules)
		if hits == 0 {
			return nil
		}
		log.Warn("sensitive words detected", "request_id", req.ID, "channel_id", target.ChannelID, "count", hits)
		if !configuration.stop {
			return nil
		}
		return &UpstreamError{Status: http.StatusForbidden, Type: "permission_error", Code: "sensitive_words_detected", Message: "sensitive words detected"}
	}
}

// sensitiveWordInterceptionEnabled reads the per-channel opt-out, defaulting
// to enabled when unset.
func sensitiveWordInterceptionEnabled(target Target) (bool, error) {
	value, present := target.Settings["sensitive_word_interception_enabled"]
	if !present || value == nil {
		return true, nil
	}
	enabled, valid := value.(bool)
	if !valid {
		return false, sensitivePolicyUnavailable()
	}
	return enabled, nil
}

// countSensitiveWordHits counts how many configured rules match text,
// case-folding substring rules but matching regex rules against the
// original text.
func countSensitiveWordHits(text string, rules []sensitiveRule) int {
	folded := strings.ToLower(text)
	hits := 0
	for _, rule := range rules {
		if rule.regex != nil {
			if rule.regex.MatchString(text) {
				hits++
			}
		} else if rule.substring != "" && strings.Contains(folded, rule.substring) {
			hits++
		}
	}
	return hits
}

func (state *sensitivePolicyState) configuration(values map[string]json.RawMessage) (*sensitiveConfiguration, error) {
	hash := sha256.New()
	for _, name := range []string{"CheckSensitiveEnabled", "CheckSensitiveOnPromptEnabled", "StopOnSensitiveEnabled", "SensitiveWords"} {
		_, _ = hash.Write([]byte(name))
		_, _ = hash.Write([]byte{0})
		if _, present := values[name]; present {
			_, _ = hash.Write([]byte{1})
		} else {
			_, _ = hash.Write([]byte{0})
		}
		_, _ = hash.Write(values[name])
		_, _ = hash.Write([]byte{0})
	}
	var key [32]byte
	copy(key[:], hash.Sum(nil))
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.loaded || state.key != key {
		state.current, state.err = loadSensitiveConfiguration(values)
		state.loaded, state.key = true, key
	}
	return state.current, state.err
}

func sensitivePolicyUnavailable() *UpstreamError {
	return &UpstreamError{Status: http.StatusServiceUnavailable, Type: "api_error", Code: "target_policy_unavailable", Message: "channel policy is unavailable"}
}
