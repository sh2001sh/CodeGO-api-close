package securityaudit

import (
	"context"
	"time"
)

type Mode string

const (
	ModeOff      Mode = "off"
	ModeAsync    Mode = "async"
	ModeBlocking Mode = "blocking"
)

type DecisionKind string

const (
	DecisionAllow       DecisionKind = "allow"
	DecisionFlag        DecisionKind = "flag"
	DecisionBlock       DecisionKind = "block"
	DecisionUnavailable DecisionKind = "unavailable"
	DecisionInvalid     DecisionKind = "invalid"
)

const (
	ErrorCodeBlocked         = "prompt_guard_blocked"
	ErrorCodeUnavailable     = "prompt_guard_unavailable"
	ErrorCodeInvalidResponse = "prompt_guard_invalid_response"
	DefaultModel             = "Qwen/Qwen3Guard-Gen-0.6B"
)

type Request struct {
	RequestID    string
	Group        string
	Protocol     string
	Model        string
	Body         []byte
	FallbackText string
	Stage        string
}

func (r Request) clone() Request {
	r.Body = append([]byte(nil), r.Body...)
	return r
}

type Snapshot struct {
	RequestID       string
	Group           string
	Protocol        string
	Model           string
	Stage           string
	PromptHash      string
	PromptLength    int
	MessageCount    int
	RedactedPreview string
	ScanText        string
}

type GuardResult struct {
	Safety          string
	Categories      []string
	MatchedScanners []string
	Unknown         []string
	Action          string
	Endpoint        string
	Latency         time.Duration
	Chunks          int
}

type Decision struct {
	Kind           DecisionKind
	ErrorCode      string
	AllowNextStage bool
	Result         *GuardResult
}

type Scanner interface {
	Scan(ctx context.Context, endpoint Endpoint, text string, scanners []string) (*GuardResult, error)
}
