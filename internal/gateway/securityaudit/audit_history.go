package securityaudit

import (
	"sync"
	"time"
)

const defaultAuditHistoryCapacity = 512

// AuditRecord is a content-free, root-visible record of one Guard decision.
// It deliberately excludes raw prompts, previews, credentials, and user data.
type AuditRecord struct {
	At           time.Time    `json:"at"`
	RequestID    string       `json:"request_id,omitempty"`
	PromptHash   string       `json:"prompt_hash"`
	PromptLength int          `json:"prompt_length"`
	MessageCount int          `json:"message_count"`
	Group        string       `json:"group,omitempty"`
	Model        string       `json:"model,omitempty"`
	Protocol     string       `json:"protocol,omitempty"`
	Stage        string       `json:"stage,omitempty"`
	Decision     DecisionKind `json:"decision"`
	ErrorCode    string       `json:"error_code,omitempty"`
	Categories   []string     `json:"categories,omitempty"`
	Endpoint     string       `json:"endpoint,omitempty"`
	LatencyMS    int64        `json:"latency_ms"`
}

type auditHistory struct {
	mu       sync.RWMutex
	entries  []AuditRecord
	capacity int
	next     int
	full     bool
}

func newAuditHistory(capacity int) *auditHistory {
	if capacity <= 0 {
		capacity = defaultAuditHistoryCapacity
	}
	return &auditHistory{entries: make([]AuditRecord, capacity), capacity: capacity}
}

func newAuditRecord(snapshot Snapshot, decision Decision, latency time.Duration) AuditRecord {
	record := AuditRecord{
		At:           time.Now().UTC(),
		RequestID:    snapshot.RequestID,
		PromptHash:   snapshot.PromptHash,
		PromptLength: snapshot.PromptLength,
		MessageCount: snapshot.MessageCount,
		Group:        snapshot.Group,
		Model:        snapshot.Model,
		Protocol:     snapshot.Protocol,
		Stage:        snapshot.Stage,
		Decision:     decision.Kind,
		ErrorCode:    decision.ErrorCode,
		LatencyMS:    latency.Milliseconds(),
	}
	if decision.Result != nil {
		record.Categories = append([]string(nil), decision.Result.Categories...)
		record.Endpoint = decision.Result.Endpoint
	}
	return record
}

func (h *auditHistory) add(record AuditRecord) {
	if h == nil || h.capacity == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries[h.next] = record
	h.next = (h.next + 1) % h.capacity
	if h.next == 0 {
		h.full = true
	}
}

func (h *auditHistory) records(limit int) []AuditRecord {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	count := h.next
	if h.full {
		count = h.capacity
	}
	if limit > 0 && limit < count {
		count = limit
	}
	result := make([]AuditRecord, 0, count)
	for offset := 0; offset < count; offset++ {
		index := h.next - 1 - offset
		if index < 0 {
			index += h.capacity
		}
		record := h.entries[index]
		record.Categories = append([]string(nil), record.Categories...)
		result = append(result, record)
	}
	return result
}
