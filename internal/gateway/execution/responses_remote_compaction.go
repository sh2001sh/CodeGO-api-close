package execution

import (
	"bytes"
	"fmt"

	"github.com/gin-gonic/gin"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/tidwall/sjson"
)

// buildRemoteCompactionV2Body preserves client request fields required by Codex.
func buildRemoteCompactionV2Body(c *gin.Context, originalModel string, mappedModel string) (*bytes.Reader, int64, error) {
	storage, err := platformhttpx.GetBodyStorage(c)
	if err != nil {
		return nil, 0, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, 0, err
	}
	if originalModel != mappedModel {
		body, err = sjson.SetBytes(body, "model", mappedModel)
		if err != nil {
			return nil, 0, fmt.Errorf("rewrite mapped response model: %w", err)
		}
	}
	return bytes.NewReader(body), int64(len(body)), nil
}
