package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHasCompactionOutputRequiresCompactionItem(t *testing.T) {
	require.True(t, hasCompactionOutput([]byte(`[{"type":"compaction","encrypted_content":"opaque"}]`)))
	require.True(t, hasCompactionOutput([]byte(`[{"type":"compaction_summary"}]`)))
	require.False(t, hasCompactionOutput([]byte(`[{"type":"message"}]`)))
	require.False(t, hasCompactionOutput(nil))
}

func TestOaiResponsesCompactionHandlerRejectsEmptyOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_1","output":[]}`)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	usage, err := OaiResponsesCompactionHandler(c, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
}

func TestOaiResponsesCompactionHandlerAcceptsCompactionOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_1","output":[{"type":"compaction","encrypted_content":"opaque"}],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}`)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	usage, err := OaiResponsesCompactionHandler(c, resp)
	require.Nil(t, err)
	require.Equal(t, 6, usage.TotalTokens)
}
