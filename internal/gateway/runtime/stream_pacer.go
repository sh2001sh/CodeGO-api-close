package runtime

import (
	"context"
	"strings"
	"time"
	"unicode"
)

const (
	firstStreamChunkTokenBudget = 16
	streamChunkTokenBudget      = 128
)

// StreamPacer splits large GPT deltas into protocol-safe fragments and tracks
// visible output. It never delays fragments: upstream flow control is the only
// pacing source.
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
	if err := ctx.Err(); err != nil {
		return err
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
	return nil
}

// OutputTokens returns the estimated visible text tokens released to the client.
func (p *StreamPacer) OutputTokens() int {
	if p == nil {
		return 0
	}
	return p.estimatedTokens
}

// OutputDuration measures the interval from the first visible text fragment
// through the final fragment released to the client.
func (p *StreamPacer) OutputDuration() (time.Duration, bool) {
	if p == nil || !p.started {
		return 0, false
	}
	return time.Since(p.firstContentAt), true
}

// SplitText breaks oversized text deltas into small, protocol-safe fragments.
// Every fragment is emitted immediately.
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
			budget = streamChunkTokenBudget
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
