package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func TestDecompressRequestMiddlewareSupportsZstd(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var compressed bytes.Buffer
	encoder, err := zstd.NewWriter(&compressed)
	require.NoError(t, err)
	_, err = encoder.Write([]byte(`{"model":"gpt-5.6-sol"}`))
	require.NoError(t, err)
	require.NoError(t, encoder.Close())

	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.POST("/v1/responses", func(c *gin.Context) {
		body, readErr := io.ReadAll(c.Request.Body)
		require.NoError(t, readErr)
		require.Empty(t, c.GetHeader("Content-Encoding"))
		c.Data(http.StatusOK, "application/json", body)
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(compressed.Bytes()))
	request.Header.Set("Content-Encoding", "zstd")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"model":"gpt-5.6-sol"}`, recorder.Body.String())
}

func TestDecompressRequestMiddlewareRejectsInvalidZstd(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.POST("/v1/responses", func(c *gin.Context) {
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString("not-zstd"))
	request.Header.Set("Content-Encoding", "zstd")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
