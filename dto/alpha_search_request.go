package dto

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/types"
)

// AlphaSearchRequest preserves the evolving Codex standalone search payload.
type AlphaSearchRequest struct {
	Model   string          `json:"model"`
	ID      string          `json:"id,omitempty"`
	Stream  *bool           `json:"stream,omitempty"`
	RawBody json.RawMessage `json:"-"`
}

func (r *AlphaSearchRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{
		CombineText: string(r.RawBody),
		TokenType:   types.TokenTypeTokenizer,
	}
}

func (r *AlphaSearchRequest) IsStream(_ *gin.Context) bool {
	return false
}

func (r *AlphaSearchRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
