package securityaudit

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeout            = 1500 * time.Millisecond
	defaultInputLimit         = 2000
	defaultQueueCapacity      = 256
	defaultWorkerCount        = 1
	defaultGlobalConcurrency  = 1
	defaultPerNodeConcurrency = 1
)

var allScannerIDs = []string{
	"violent",
	"non_violent_illegal_acts",
	"sexual_content_or_sexual_acts",
	"pii",
	"suicide_and_self_harm",
	"unethical_acts",
	"politically_sensitive_topics",
	"copyright_violation",
	"jailbreak",
}

type Endpoint struct {
	ID         string `json:"id"`
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key"`
	Model      string `json:"model"`
	TimeoutMS  int    `json:"timeout_ms"`
	InputLimit int    `json:"input_limit"`
}

func (e Endpoint) timeout() time.Duration {
	if e.TimeoutMS <= 0 {
		return defaultTimeout
	}
	return time.Duration(e.TimeoutMS) * time.Millisecond
}

func (e Endpoint) inputLimit() int {
	if e.InputLimit <= 0 {
		return defaultInputLimit
	}
	return e.InputLimit
}

type Config struct {
	Mode               Mode
	Groups             []string
	Scanners           []string
	Endpoints          []Endpoint
	LatestTurnOnly     bool
	BlockControversial bool
	QueueCapacity      int
	WorkerCount        int
	GlobalConcurrency  int
	PerNodeConcurrency int
}

func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Mode:               normalizeMode(os.Getenv("PROMPT_AUDIT_MODE")),
		Groups:             splitCSV(os.Getenv("PROMPT_AUDIT_GROUPS")),
		Scanners:           normalizeScanners(splitCSV(os.Getenv("PROMPT_AUDIT_SCANNERS"))),
		LatestTurnOnly:     envBool("PROMPT_AUDIT_LATEST_TURN_ONLY", true),
		BlockControversial: envBool("PROMPT_AUDIT_BLOCK_CONTROVERSIAL", false),
		QueueCapacity:      envInt("PROMPT_AUDIT_QUEUE_CAPACITY", defaultQueueCapacity),
		WorkerCount:        envInt("PROMPT_AUDIT_WORKERS", defaultWorkerCount),
		GlobalConcurrency:  envInt("PROMPT_AUDIT_GLOBAL_CONCURRENCY", defaultGlobalConcurrency),
		PerNodeConcurrency: envInt("PROMPT_AUDIT_PER_NODE_CONCURRENCY", defaultPerNodeConcurrency),
	}
	if raw := strings.TrimSpace(os.Getenv("PROMPT_AUDIT_ENDPOINTS_JSON")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.Endpoints); err != nil {
			return Config{}, fmt.Errorf("decode PROMPT_AUDIT_ENDPOINTS_JSON: %w", err)
		}
	} else if baseURL := strings.TrimSpace(os.Getenv("PROMPT_AUDIT_BASE_URL")); baseURL != "" {
		cfg.Endpoints = []Endpoint{{
			ID: "primary", BaseURL: baseURL, APIKey: os.Getenv("PROMPT_AUDIT_API_KEY"),
			Model:      strings.TrimSpace(os.Getenv("PROMPT_AUDIT_MODEL")),
			TimeoutMS:  envInt("PROMPT_AUDIT_TIMEOUT_MS", int(defaultTimeout/time.Millisecond)),
			InputLimit: envInt("PROMPT_AUDIT_INPUT_LIMIT", defaultInputLimit),
		}}
	}
	normalizeConfig(&cfg)
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func normalizeConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	if len(cfg.Scanners) == 0 {
		cfg.Scanners = append([]string(nil), allScannerIDs...)
	}
	if cfg.QueueCapacity <= 0 {
		cfg.QueueCapacity = defaultQueueCapacity
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = defaultWorkerCount
	}
	if cfg.GlobalConcurrency <= 0 {
		cfg.GlobalConcurrency = defaultGlobalConcurrency
	}
	if cfg.PerNodeConcurrency <= 0 {
		cfg.PerNodeConcurrency = defaultPerNodeConcurrency
	}
	for index := range cfg.Endpoints {
		endpoint := &cfg.Endpoints[index]
		endpoint.ID = strings.TrimSpace(endpoint.ID)
		if endpoint.ID == "" {
			endpoint.ID = fmt.Sprintf("guard-%d", index+1)
		}
		endpoint.BaseURL = strings.TrimSpace(endpoint.BaseURL)
		endpoint.Model = strings.TrimSpace(endpoint.Model)
		if endpoint.Model == "" {
			endpoint.Model = DefaultModel
		}
		if endpoint.TimeoutMS <= 0 {
			endpoint.TimeoutMS = int(defaultTimeout / time.Millisecond)
		}
		if endpoint.InputLimit <= 0 {
			endpoint.InputLimit = defaultInputLimit
		}
	}
}

func validateConfig(cfg Config) error {
	if cfg.Mode == ModeOff {
		return nil
	}
	if len(cfg.Endpoints) == 0 {
		return fmt.Errorf("prompt audit %s mode requires at least one endpoint", cfg.Mode)
	}
	if cfg.WorkerCount > 32 || cfg.QueueCapacity > 100000 {
		return fmt.Errorf("prompt audit worker or queue limit is too large")
	}
	for _, endpoint := range cfg.Endpoints {
		if _, err := chatCompletionsURL(endpoint.BaseURL); err != nil {
			return fmt.Errorf("prompt audit endpoint %s: %w", endpoint.ID, err)
		}
		if endpoint.TimeoutMS < 100 || endpoint.TimeoutMS > 30000 {
			return fmt.Errorf("prompt audit endpoint %s timeout is out of range", endpoint.ID)
		}
		if endpoint.InputLimit < 128 || endpoint.InputLimit > 100000 {
			return fmt.Errorf("prompt audit endpoint %s input limit is out of range", endpoint.ID)
		}
	}
	return nil
}

func (cfg Config) includesGroup(group string) bool {
	if len(cfg.Groups) == 0 {
		return true
	}
	group = strings.ToLower(strings.TrimSpace(group))
	for _, allowed := range cfg.Groups {
		if allowed == "*" || strings.EqualFold(allowed, group) {
			return true
		}
	}
	return false
}

func normalizeMode(raw string) Mode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "async", "async_audit", "shadow":
		return ModeAsync
	case "blocking", "block":
		return ModeBlocking
	default:
		return ModeOff
	}
}

func normalizeModeFromEnvironment() Mode {
	return normalizeMode(os.Getenv("PROMPT_AUDIT_MODE"))
}

func normalizeScanners(values []string) []string {
	known := make(map[string]struct{}, len(allScannerIDs))
	for _, id := range allScannerIDs {
		known[id] = struct{}{}
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = normalizeCategory(value)
		if _, ok := known[value]; !ok {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(key)))
	if err != nil {
		return fallback
	}
	return value
}
