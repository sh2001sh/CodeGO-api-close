package securityaudit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

const maxGuardResponseBytes = 256 << 10

type GuardError struct {
	Code      string
	Retryable bool
	Cause     error
}

func (e *GuardError) Error() string { return e.Code }
func (e *GuardError) Unwrap() error { return e.Cause }

type QwenScanner struct {
	clients sync.Map
}

func NewQwenScanner() *QwenScanner { return &QwenScanner{} }

func (s *QwenScanner) Scan(ctx context.Context, endpoint Endpoint, text string, scanners []string) (*GuardResult, error) {
	requestURL, err := chatCompletionsURL(endpoint.BaseURL)
	if err != nil {
		return nil, &GuardError{Code: ErrorCodeUnavailable, Cause: err}
	}
	payload := map[string]any{
		"model": endpoint.Model, "messages": []map[string]string{{"role": "user", "content": text}},
		"temperature": 0, "max_tokens": 64, "seed": 42,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &GuardError{Code: ErrorCodeInvalidResponse, Cause: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, &GuardError{Code: ErrorCodeUnavailable, Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	if endpoint.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+endpoint.APIKey)
	}
	resp, err := s.client(endpoint).Do(req)
	if err != nil {
		return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: true, Cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &GuardError{
			Code: ErrorCodeUnavailable, Retryable: resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
		}
	}
	limited := io.LimitReader(resp.Body, maxGuardResponseBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil || len(responseBody) > maxGuardResponseBytes {
		return nil, &GuardError{Code: ErrorCodeInvalidResponse, Cause: err}
	}
	content, err := extractResponseContent(responseBody)
	if err != nil {
		return nil, &GuardError{Code: ErrorCodeInvalidResponse, Cause: err}
	}
	result, err := ParseQwen3Guard(content, scanners)
	if err != nil {
		return nil, err
	}
	result.Endpoint = endpoint.ID
	return result, nil
}

func (s *QwenScanner) client(endpoint Endpoint) *http.Client {
	key := endpoint.ID + "\x00" + endpoint.BaseURL + "\x00" + endpoint.timeout().String()
	if existing, found := s.clients.Load(key); found {
		return existing.(*http.Client)
	}
	client := &http.Client{Timeout: endpoint.timeout()}
	actual, _ := s.clients.LoadOrStore(key, client)
	return actual.(*http.Client)
}

func ParseQwen3Guard(content string, enabledScanners []string) (*GuardResult, error) {
	safety, categoriesLine, err := parseGuardLines(content)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(safety) {
	case "safe":
		safety = "Safe"
	case "controversial":
		safety = "Controversial"
	case "unsafe":
		safety = "Unsafe"
	default:
		return nil, &GuardError{Code: ErrorCodeInvalidResponse}
	}
	if categoriesLine == "" {
		return nil, &GuardError{Code: ErrorCodeInvalidResponse}
	}
	known, matched, unknown := classifyGuardCategories(categoriesLine, enabledScanners)
	return &GuardResult{
		Safety: safety, Categories: known, MatchedScanners: matched,
		Unknown: unknown, Action: guardAction(safety, known, matched, unknown),
	}, nil
}

func parseGuardLines(content string) (string, string, error) {
	var safety, categories string
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "safety:"):
			if safety != "" {
				return "", "", &GuardError{Code: ErrorCodeInvalidResponse}
			}
			safety = strings.TrimSpace(line[len("safety:"):])
		case strings.HasPrefix(lower, "categories:"):
			if categories != "" {
				return "", "", &GuardError{Code: ErrorCodeInvalidResponse}
			}
			categories = strings.TrimSpace(line[len("categories:"):])
		default:
			return "", "", &GuardError{Code: ErrorCodeInvalidResponse}
		}
	}
	return safety, categories, nil
}

func classifyGuardCategories(categoriesLine string, enabledScanners []string) (known, matched, unknown []string) {
	enabled := make(map[string]struct{}, len(enabledScanners))
	for _, scanner := range enabledScanners {
		enabled[normalizeCategory(scanner)] = struct{}{}
	}
	knownCatalog := make(map[string]struct{}, len(allScannerIDs))
	for _, scanner := range allScannerIDs {
		knownCatalog[scanner] = struct{}{}
	}
	for _, raw := range strings.Split(categoriesLine, ",") {
		category := normalizeCategory(raw)
		if category == "" || category == "none" || category == "n_a" {
			continue
		}
		if _, ok := knownCatalog[category]; !ok {
			digest := sha256.Sum256([]byte(category))
			unknown = append(unknown, fmt.Sprintf("unknown:%x", digest[:8]))
			continue
		}
		known = append(known, category)
		if _, ok := enabled[category]; ok {
			matched = append(matched, category)
		}
	}
	known, matched, unknown = uniqueSorted(known), uniqueSorted(matched), uniqueSorted(unknown)
	return known, matched, unknown
}

func guardAction(safety string, known, matched, unknown []string) string {
	action := "allow"
	if safety == "Controversial" {
		action = "warn"
	}
	if safety == "Unsafe" {
		action = "warn"
		if len(matched) > 0 || len(unknown) > 0 || len(known) == 0 {
			action = "block"
		}
	}
	return action
}

func extractResponseContent(body []byte) (string, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &response); err != nil || len(response.Choices) == 0 {
		return "", errors.New("prompt guard response envelope invalid")
	}
	switch content := response.Choices[0].Message.Content.(type) {
	case string:
		if strings.TrimSpace(content) == "" {
			return "", errors.New("prompt guard response content empty")
		}
		return content, nil
	case []any:
		parts := make([]string, 0, len(content))
		for _, item := range content {
			object, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := object["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n"), nil
		}
	}
	return "", errors.New("prompt guard response content invalid")
}

func chatCompletionsURL(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("base URL is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("base URL scheme must be http or https")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("base URL must not contain userinfo, query, or fragment")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/chat/completions") {
		return parsed.String(), nil
	}
	if strings.HasSuffix(path, "/v1") {
		parsed.Path = path + "/chat/completions"
	} else {
		parsed.Path = path + "/v1/chat/completions"
	}
	return parsed.String(), nil
}

func normalizeCategory(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("_", " ", "&", " and ", "/", " ", "-", " ", "–", " ", "—", " ").Replace(value)
	value = strings.Join(strings.Fields(value), " ")
	aliases := map[string]string{
		"violence": "violent", "non violent illegal acts": "non_violent_illegal_acts",
		"sexual": "sexual_content_or_sexual_acts", "sexual content or sexual acts": "sexual_content_or_sexual_acts",
		"personal identifying information": "pii", "personal identifiable information": "pii",
		"suicide self harm": "suicide_and_self_harm", "suicide and self harm": "suicide_and_self_harm",
		"unethical": "unethical_acts", "unethical acts": "unethical_acts",
		"political": "politically_sensitive_topics", "politically sensitive topics": "politically_sensitive_topics",
		"copyright": "copyright_violation", "copyright violation": "copyright_violation",
		"prompt injection": "jailbreak",
	}
	if canonical, found := aliases[value]; found {
		return canonical
	}
	return strings.ReplaceAll(value, " ", "_")
}

func containsElevatedCategory(values []string) bool {
	for _, value := range values {
		if value == "jailbreak" || value == "pii" || value == "suicide_and_self_harm" {
			return true
		}
	}
	return false
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, found := seen[value]; !found {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
