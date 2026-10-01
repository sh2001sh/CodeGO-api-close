package dify

import (
	"fmt"
	"math"
	"strconv"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type chatUsage struct {
	Prompt            int64          `json:"prompt_tokens"`
	Completion        int64          `json:"completion_tokens"`
	Total             int64          `json:"total_tokens"`
	PromptDetails     *inputDetails  `json:"prompt_tokens_details,omitempty"`
	CompletionDetails *outputDetails `json:"completion_tokens_details,omitempty"`
}

type inputDetails struct {
	Cached     int64 `json:"cached_tokens,omitempty"`
	CacheWrite int64 `json:"cache_write_tokens,omitempty"`
	Audio      int64 `json:"audio_tokens,omitempty"`
	Image      int64 `json:"image_tokens,omitempty"`
}

type outputDetails struct {
	Audio int64 `json:"audio_tokens,omitempty"`
	Image int64 `json:"image_tokens,omitempty"`
}

func usageJSON(u *gateway.Usage) *chatUsage {
	if u == nil {
		return nil
	}
	out := &chatUsage{Prompt: u.PromptTokens, Completion: u.CompletionTokens, Total: u.PromptTokens + u.CompletionTokens}
	if u.CachedTokens != 0 || u.CacheWriteTokens != 0 || u.AudioInputTokens != 0 || u.ImageInputTokens != 0 {
		out.PromptDetails = &inputDetails{Cached: u.CachedTokens, CacheWrite: u.CacheWriteTokens, Audio: u.AudioInputTokens, Image: u.ImageInputTokens}
	}
	if u.AudioOutputTokens != 0 || u.ImageOutputTokens != 0 {
		out.CompletionDetails = &outputDetails{Audio: u.AudioOutputTokens, Image: u.ImageOutputTokens}
	}
	return out
}

func parseUsage(root gjson.Result) (*gateway.Usage, error) {
	if !root.Exists() || root.Type == gjson.Null {
		return nil, nil
	}
	if !root.IsObject() {
		return nil, fmt.Errorf("dify: usage must be an object")
	}
	p, c, hasCore, err := parseCoreTokenCounts(root)
	if err != nil {
		return nil, err
	}
	if !hasCore {
		return nil, nil
	}
	usage := &gateway.Usage{PromptTokens: p, CompletionTokens: c}
	if err := parseTokenDetails(root, usage); err != nil {
		return nil, err
	}
	if err := parseCacheWriteTokens(root, usage); err != nil {
		return nil, err
	}
	return usage, nil
}

// parseCoreTokenCounts validates and extracts prompt/completion/total token
// counts, checking total_tokens for consistency when present. hasCore is
// false when either prompt or completion tokens are absent.
func parseCoreTokenCounts(root gjson.Result) (prompt, completion int64, hasCore bool, err error) {
	counts := map[string]int64{}
	for _, key := range []string{"prompt_tokens", "completion_tokens", "total_tokens"} {
		v := root.Get(key)
		if !v.Exists() || v.Type == gjson.Null {
			continue
		}
		n, err := strconv.ParseInt(v.Raw, 10, 64)
		if v.Type != gjson.Number || err != nil || n < 0 {
			return 0, 0, false, fmt.Errorf("dify: returned invalid %s", key)
		}
		counts[key] = n
	}
	p, hasPrompt := counts["prompt_tokens"]
	c, hasCompletion := counts["completion_tokens"]
	if !hasPrompt || !hasCompletion {
		return 0, 0, false, nil
	}
	if p > math.MaxInt64-c {
		return 0, 0, false, fmt.Errorf("dify: token total overflows")
	}
	if total, ok := counts["total_tokens"]; ok && total != p+c {
		return 0, 0, false, fmt.Errorf("dify: returned inconsistent token totals")
	}
	return p, c, true, nil
}

// parseTokenDetails fills the cached/audio/image token detail fields that
// map directly from a single JSON path.
func parseTokenDetails(root gjson.Result, usage *gateway.Usage) error {
	for path, dest := range map[string]*int64{
		"prompt_tokens_details.cached_tokens":    &usage.CachedTokens,
		"prompt_tokens_details.audio_tokens":     &usage.AudioInputTokens,
		"prompt_tokens_details.image_tokens":     &usage.ImageInputTokens,
		"completion_tokens_details.audio_tokens": &usage.AudioOutputTokens,
		"completion_tokens_details.image_tokens": &usage.ImageOutputTokens,
		"claude_cache_creation_1_h_tokens":       &usage.CacheWrite1hTokens,
	} {
		n, err := usageCount(root.Get(path), path)
		if err != nil {
			return err
		}
		*dest = n
	}
	if !root.Get("prompt_tokens_details.cached_tokens").Exists() {
		n, err := usageCount(root.Get("prompt_cache_hit_tokens"), "prompt_cache_hit_tokens")
		if err != nil {
			return err
		}
		usage.CachedTokens = n
	}
	return nil
}

// parseCacheWriteTokens fills CacheWriteTokens from the first matching
// provider-specific path, falling back to Claude's 5-minute cache creation
// count combined with the already-parsed 1-hour count.
func parseCacheWriteTokens(root gjson.Result, usage *gateway.Usage) error {
	for _, path := range []string{"prompt_tokens_details.cached_creation_tokens", "prompt_tokens_details.cache_creation_tokens", "prompt_tokens_details.cache_creation_input_tokens", "prompt_tokens_details.cache_write_tokens", "prompt_tokens_details.cache_write_input_tokens"} {
		n, err := usageCount(root.Get(path), path)
		if err != nil {
			return err
		}
		if usage.CacheWriteTokens == 0 {
			usage.CacheWriteTokens = n
		}
	}
	if usage.CacheWriteTokens == 0 {
		five, err := usageCount(root.Get("claude_cache_creation_5_m_tokens"), "claude_cache_creation_5_m_tokens")
		if err != nil {
			return err
		}
		if five > math.MaxInt64-usage.CacheWrite1hTokens {
			return fmt.Errorf("dify: cache token total overflows")
		}
		usage.CacheWriteTokens = five + usage.CacheWrite1hTokens
	}
	return nil
}

func usageCount(value gjson.Result, path string) (int64, error) {
	if !value.Exists() || value.Type == gjson.Null {
		return 0, nil
	}
	n, err := strconv.ParseInt(value.Raw, 10, 64)
	if err != nil || value.Type != gjson.Number || n < 0 {
		return 0, fmt.Errorf("dify: returned invalid %s", path)
	}
	return n, nil
}
