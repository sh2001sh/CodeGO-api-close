package httpx

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	fastjson "github.com/goccy/go-json"
	"github.com/stretchr/testify/require"
)

func TestUnmarshalBodyReusableFastJSONPreservesReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := []byte(`{"model":"gpt-5.6-sol","input":[{"role":"user","content":"hello"}]}`)
	request := httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	t.Cleanup(func() { CleanupBodyStorage(context) })

	var payload struct {
		Model string `json:"model"`
		Input []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"input"`
	}
	require.NoError(t, UnmarshalBodyReusable(context, &payload))
	require.Equal(t, "gpt-5.6-sol", payload.Model)
	require.Equal(t, "hello", payload.Input[0].Content)

	replayed, err := io.ReadAll(context.Request.Body)
	require.NoError(t, err)
	require.Equal(t, raw, replayed)
}

func TestUnmarshalBodyReusableFastJSONRejectsTrailingData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"gpt-5.6-sol"} trailing`))
	request.Header.Set("Content-Type", "application/json")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	t.Cleanup(func() { CleanupBodyStorage(context) })

	var payload map[string]any
	require.Error(t, UnmarshalBodyReusable(context, &payload))
}

func BenchmarkUnmarshalBodyReusableLargeJSON(b *testing.B) {
	gin.SetMode(gin.TestMode)
	raw := []byte(`{"model":"gpt-5.6-sol","input":"` + strings.Repeat("long prompt content ", 55_000) + `"}`)
	type payload struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}
	b.Run("stdlib", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(raw)))
		for i := 0; i < b.N; i++ {
			var decoded payload
			if err := json.Unmarshal(raw, &decoded); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("fast", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(raw)))
		for i := 0; i < b.N; i++ {
			var decoded payload
			if err := fastjson.Unmarshal(raw, &decoded); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("reusable_request", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(raw)))
		for i := 0; i < b.N; i++ {
			request := httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(raw))
			request.Header.Set("Content-Type", "application/json")
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = request
			var decoded payload
			if err := UnmarshalBodyReusable(context, &decoded); err != nil {
				b.Fatal(err)
			}
			CleanupBodyStorage(context)
		}
	})
}
