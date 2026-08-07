package stream

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"github.com/sh2001sh/new-api/types"
)

// AttemptStage describes how far an upstream attempt has progressed. Only a
// Responses stream before semanticCommitted may be replayed safely.
type AttemptStage string

const (
	AttemptStageSelected          AttemptStage = "selected"
	AttemptStageConnected         AttemptStage = "connected"
	AttemptStageBootstrap         AttemptStage = "bootstrap"
	AttemptStageSemanticCommitted AttemptStage = "semantic_committed"
	AttemptStageCompleted         AttemptStage = "completed"
)

func BeginRelayAttempt(c *gin.Context) {
	setAttemptStage(c, AttemptStageSelected)
}

func MarkAttemptConnected(c *gin.Context) {
	setAttemptStage(c, AttemptStageConnected)
}

func MarkAttemptBootstrap(c *gin.Context) {
	setAttemptStage(c, AttemptStageBootstrap)
}

// MarkSemanticCommitted must run before writing a model delta or tool call.
// A partial write can still be visible to a client, so retries are unsafe once
// this transition happens.
func MarkSemanticCommitted(c *gin.Context) {
	setAttemptStage(c, AttemptStageSemanticCommitted)
	if c != nil {
		c.Set(string(constant.ContextKeyStreamContentDelivered), true)
	}
}

func MarkAttemptCompleted(c *gin.Context) {
	setAttemptStage(c, AttemptStageCompleted)
}

func AttemptStageFromContext(c *gin.Context) AttemptStage {
	if c == nil {
		return AttemptStageSelected
	}
	stage, ok := c.Get(string(constant.ContextKeyRelayAttemptStage))
	if !ok {
		return AttemptStageSelected
	}
	value, ok := stage.(AttemptStage)
	if !ok {
		return AttemptStageSelected
	}
	return value
}

func CanRetryResponsesBeforeSemanticOutput(c *gin.Context) bool {
	if c == nil || !c.GetBool(string(constant.ContextKeyResponsesStreamRetrySafe)) {
		return false
	}
	return AttemptStageFromContext(c) != AttemptStageSemanticCommitted &&
		AttemptStageFromContext(c) != AttemptStageCompleted &&
		!c.GetBool(string(constant.ContextKeyStreamContentDelivered))
}

func WriteOpenAIStreamError(c *gin.Context, openAIError types.OpenAIError, responses bool) error {
	payload, err := platformencoding.Marshal(struct {
		Error types.OpenAIError `json:"error"`
	}{Error: openAIError})
	if err != nil {
		return fmt.Errorf("marshal stream error: %w", err)
	}
	if responses {
		c.Render(-1, CustomEvent{Data: "event: error\n"})
		c.Render(-1, CustomEvent{Data: "data: " + string(payload)})
		return FlushWriter(c)
	}
	if err := StringData(c, string(payload)); err != nil {
		return err
	}
	return StringData(c, "[DONE]")
}

func WriteClaudeStreamError(c *gin.Context, claudeError types.ClaudeError) error {
	payload, err := platformencoding.Marshal(struct {
		Type  string            `json:"type"`
		Error types.ClaudeError `json:"error"`
	}{
		Type:  "error",
		Error: claudeError,
	})
	if err != nil {
		return fmt.Errorf("marshal claude stream error: %w", err)
	}
	c.Render(-1, CustomEvent{Data: "event: error\n"})
	c.Render(-1, CustomEvent{Data: "data: " + string(payload)})
	return FlushWriter(c)
}

func setAttemptStage(c *gin.Context, stage AttemptStage) {
	if c != nil {
		c.Set(string(constant.ContextKeyRelayAttemptStage), stage)
	}
}
