package runtime

import (
	"context"
	"strings"
	"time"
	"unicode"
)

const gptStreamTargetTokensPerSecond = 50

const (
	firstStreamChunkTokenBudget = 4
	streamChunkTokenBudget      = 8
)

// StreamPacer keeps GPT text output in a believable sustained range after the
// first text delta. Control frames and the first delta remain immediate.
type StreamPacer struct {
	started         bool
	firstContentAt  time.Time
	estimatedTokens int
}

func NewStreamPacer(modelName string) *StreamPacer {
	if !strings.HasPrefix(strings.ToLower(modelName), "gpt") {
		return nil
	}
	return &StreamPacer{}
}

func (p *StreamPacer) Pace(ctx context.Context, text string) error {
	if p == nil || text == "" {
		return nil
	}
	tokens := estimateStreamTokens(text)
	if tokens <= 0 {
		return nil
	}
	if !p.started {
		p.started = true
		p.firstContentAt = time.Now()
		p.estimatedTokens = tokens
		return nil
	}

	p.estimatedTokens += tokens
	targetAt := p.firstContentAt.Add(
		time.Duration(p.estimatedTokens) * time.Second / gptStreamTargetTokensPerSecond,
	)
	if wait := time.Until(targetAt); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// OutputTokens returns the estimated visible text tokens released to the client.
func (p *StreamPacer) OutputTokens() int {
	if p == nil {
		return 0
	}
	return p.estimatedTokens
}

// SplitText breaks oversized text deltas into small, protocol-safe fragments.
// The first fragment is emitted immediately; later fragments are paced by Pace.
func (p *StreamPacer) SplitText(text string) []string {
	if text == "" {
		return nil
	}
	if p == nil {
		return []string{text}
	}
	budget := streamChunkTokenBudget
	if !p.started {
		budget = firstStreamChunkTokenBudget
	}
	if estimateStreamTokens(text) <= budget {
		return []string{text}
	}

	parts := make([]string, 0, len(text)/budget)
	var current strings.Builder
	tokens := 0
	latinRunes := 0
	for _, char := range text {
		current.WriteRune(char)
		if char <= unicode.MaxASCII && (unicode.IsLetter(char) || unicode.IsDigit(char)) {
			latinRunes++
		} else {
			if latinRunes > 0 {
				tokens += (latinRunes + 3) / 4
				latinRunes = 0
			}
			if !unicode.IsSpace(char) {
				tokens++
			}
		}
		currentTokens := tokens
		if latinRunes > 0 {
			currentTokens += (latinRunes + 3) / 4
		}
		if currentTokens >= budget {
			parts = append(parts, current.String())
			current.Reset()
			tokens = 0
			latinRunes = 0
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func estimateStreamTokens(text string) int {
	latinRunes := 0
	tokens := 0
	flushLatin := func() {
		if latinRunes > 0 {
			tokens += (latinRunes + 3) / 4
			latinRunes = 0
		}
	}
	for _, char := range text {
		if char <= unicode.MaxASCII && (unicode.IsLetter(char) || unicode.IsDigit(char)) {
			latinRunes++
			continue
		}
		flushLatin()
		if !unicode.IsSpace(char) {
			tokens++
		}
	}
	flushLatin()
	return tokens
}
