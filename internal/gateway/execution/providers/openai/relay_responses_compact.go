package openai

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/types"
	"io"
	"net/http"
)

func OaiResponsesCompactionHandler(c *gin.Context, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer platformhttpx.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var compactResp dto.OpenAIResponsesCompactionResponse
	if err := platformencoding.Unmarshal(responseBody, &compactResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := compactResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}
	if !hasCompactionOutput(compactResp.Output) {
		return nil, types.NewOpenAIError(
			fmt.Errorf("upstream returned a compact response without a compaction output item"),
			types.ErrorCodeBadResponseBody,
			http.StatusBadGateway,
		)
	}

	platformhttpx.IOCopyBytesGracefully(c, resp, responseBody)

	usage := dto.Usage{}
	if compactResp.Usage != nil {
		usage.PromptTokens = compactResp.Usage.InputTokens
		usage.CompletionTokens = compactResp.Usage.OutputTokens
		usage.TotalTokens = compactResp.Usage.TotalTokens
		if compactResp.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = compactResp.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.CachedCreationTokens = compactResp.Usage.InputTokensDetails.GetCachedCreationTokens()
		}
	}

	return &usage, nil
}

func hasCompactionOutput(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var items []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return false
	}
	for _, item := range items {
		if item.Type == "compaction" || item.Type == "compaction_summary" {
			return true
		}
	}
	return false
}
