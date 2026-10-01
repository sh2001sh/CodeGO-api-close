package gateway_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSensitiveGlobalTogglesBoolAndImportedString(t *testing.T) {
	for _, field := range []string{"CheckSensitiveEnabled", "CheckSensitiveOnPromptEnabled", "StopOnSensitiveEnabled"} {
		for _, value := range []any{false, "false", true, "true"} {
			t.Run(fmt.Sprintf("%s_%T_%v", field, value, value), func(t *testing.T) {
				h := newSensitiveHTTPHarness(t, sensitiveValues("contains:blocked-token", map[string]any{field: value}), nil)
				status, body, _ := h.request(t, "/v1/chat/completions", sensitiveChatBody("BLOCKED-TOKEN"))
				want := 403
				if value == false || value == "false" {
					want = 200
				}
				if status != want {
					t.Fatalf("toggle %s=%v returned %d want %d: %s", field, value, status, want, body)
				}
			})
		}
	}
}

func TestSensitiveStopFalseAuditsWithoutPromptOrRulePayload(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := newSensitiveHTTPHarness(t, sensitiveValues("private-rule-token", map[string]any{"StopOnSensitiveEnabled": false}), logger)
	status, body, out := h.request(t, "/v1/chat/completions", sensitiveChatBody("private-rule-token private-prompt-suffix"))
	if status != 200 || out == nil || !out.Charge || h.calls.Load() != 1 {
		t.Fatalf("audit mode blocked request %d %s", status, body)
	}
	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["request_id"] == nil || entry["channel_id"] != float64(1) || entry["count"] != float64(1) {
		t.Fatalf("missing safe audit identifiers: %v", entry)
	}
	if strings.Contains(logs.String(), "private-rule-token") || strings.Contains(logs.String(), "private-prompt-suffix") {
		t.Fatal("prompt or rule leaked into audit log")
	}
}

func TestSensitivePlainContainsRegexAndEmptyWords(t *testing.T) {
	for _, test := range []struct {
		name   string
		words  any
		prompt string
		want   int
	}{
		{"contains casefold", "contains:Alpha", "prefix ALPHA suffix", 403},
		{"plain casefold", []string{"Alpha"}, "prefix aLpHa suffix", 403},
		{"unicode casefold", []string{"ÄLPHA"}, "älpha", 403},
		{"regex explicit insensitive", "re:(?i)^alpha[0-9]+$", "ALPHA42", 403},
		{"regex case sensitive allowed", []string{"re:^Alpha$"}, "alpha", 200},
		{"regex case sensitive hit", []string{"re:^Alpha$"}, "Alpha", 403},
		{"newline list", "\nother\ncontains:Alpha\r\n", "ALPHA", 403},
		{"empty replacement", []string{}, "reverse shell", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newSensitiveHTTPHarness(t, sensitiveValues(test.words, nil), nil)
			status, body, _ := h.request(t, "/v1/chat/completions", sensitiveChatBody(test.prompt))
			if status != test.want {
				t.Fatalf("got %d want %d: %s", status, test.want, body)
			}
		})
	}
}

func TestSensitiveInvalidConfigurationFailsClosedBeforeReserve(t *testing.T) {
	for _, values := range []map[string]json.RawMessage{
		{"CheckSensitiveEnabled": json.RawMessage(`"not a bool"`)},
		{"CheckSensitiveOnPromptEnabled": json.RawMessage(`null`)},
		{"StopOnSensitiveEnabled": json.RawMessage(`7`)},
		{"SensitiveWords": json.RawMessage(`null`)},
		{"SensitiveWords": json.RawMessage(`[1]`)},
		{"SensitiveWords": json.RawMessage(`{"rule":"x"}`)},
		{"SensitiveWords": json.RawMessage(`"re:["`)},
		{"SensitiveWords": json.RawMessage(`"re:"`)},
		{"SensitiveWords": json.RawMessage(`"invalid JSON`)},
	} {
		h := newSensitiveHTTPHarness(t, func() map[string]json.RawMessage { return values }, nil)
		status, body, out := h.request(t, "/v1/chat/completions", sensitiveChatBody("hello"))
		if status != 503 || !strings.Contains(body, "target_policy_unavailable") || out != nil || h.reserves() != 0 || h.calls.Load() != 0 {
			t.Fatalf("invalid policy did not fail closed: %d %s", status, body)
		}
	}
}

func TestSensitiveCurrentConfigurationReloadAndRecovery(t *testing.T) {
	var current atomic.Value
	current.Store(map[string]json.RawMessage{"SensitiveWords": json.RawMessage(`"alpha"`)})
	h := newSensitiveHTTPHarness(t, func() map[string]json.RawMessage { return current.Load().(map[string]json.RawMessage) }, nil)
	assert := func(prompt string, want int) {
		t.Helper()
		status, body, _ := h.request(t, "/v1/chat/completions", sensitiveChatBody(prompt))
		if status != want {
			t.Fatalf("got %d want %d: %s", status, want, body)
		}
	}
	assert("alpha", 403)
	current.Store(map[string]json.RawMessage{"SensitiveWords": json.RawMessage(`"re:^beta$"`)})
	assert("alpha", 200)
	assert("beta", 403)
	current.Store(map[string]json.RawMessage{"SensitiveWords": json.RawMessage(`"re:["`)})
	assert("beta", 503)
	current.Store(map[string]json.RawMessage{"SensitiveWords": json.RawMessage(`"gamma"`)})
	assert("beta", 200)
	assert("gamma", 403)
	current.Store(map[string]json.RawMessage{"SensitiveWords": json.RawMessage(`"gamma"`), "CheckSensitiveEnabled": nil})
	assert("gamma", 503)
	current.Store(map[string]json.RawMessage{"SensitiveWords": json.RawMessage(`"gamma"`), "CheckSensitiveEnabled": json.RawMessage(`false`)})
	assert("gamma", 200)
}

func TestSensitiveRegexDoesNotMatchAbsentTextOrMediaURLs(t *testing.T) {
	h := newSensitiveHTTPHarness(t, sensitiveValues("re:.*", nil), nil)
	status, body, out := h.request(t, "/v1/chat/completions", `{"model":"test-model","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/anything"}}]}]}`)
	if status != 200 || out == nil || h.calls.Load() != 1 {
		t.Fatalf("regex matched non-text request: %d %s", status, body)
	}
}

func TestSensitiveConcurrentSnapshotReload(t *testing.T) {
	var current atomic.Value
	current.Store(map[string]json.RawMessage{"SensitiveWords": json.RawMessage(`"contains:alpha"`)})
	h := newSensitiveHTTPHarness(t, func() map[string]json.RawMessage { return current.Load().(map[string]json.RawMessage) }, nil)
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		for i := 0; i < 30; i++ {
			rule := `"contains:alpha"`
			if i%2 != 0 {
				rule = `"re:(?i)alpha"`
			}
			current.Store(map[string]json.RawMessage{"SensitiveWords": json.RawMessage(rule)})
		}
	}()
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			status, body, _ := h.request(t, "/v1/chat/completions", sensitiveChatBody("ALPHA"))
			if status != 403 {
				t.Errorf("concurrent policy returned %d: %s", status, body)
			}
		}()
	}
	group.Wait()
	if h.calls.Load() != 0 || h.reserves() != 0 {
		t.Fatal("concurrent blocked request forwarded or reserved")
	}
}
