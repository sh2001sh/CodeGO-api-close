package http

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	platformconcurrency "github.com/sh2001sh/new-api/internal/platform/concurrency"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

type readCountingBody struct {
	reads int
}

func (b *readCountingBody) Read([]byte) (int, error) {
	b.reads++
	return 0, io.EOF
}

func TestRelayAdmissionRejectsBeforeReadingRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	platformconcurrency.ConfigureRelayAdmission(1)
	t.Cleanup(func() { platformconcurrency.ConfigureRelayAdmission(0) })

	release, admitted, _ := platformconcurrency.TryAcquireRelaySlot()
	require.True(t, admitted)
	t.Cleanup(release)

	body := &readCountingBody{}
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", body)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request

	relayRequest(ctx, types.RelayFormatOpenAIResponses)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "1", recorder.Header().Get("Retry-After"))
	require.Zero(t, body.reads, "rejected relay must not materialize its request body")
}
