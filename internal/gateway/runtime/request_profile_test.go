package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestRequestProfileClassification(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		path     string
		hint     RequestProfileHint
		expected RequestType
	}{
		{name: "short stream", path: "/v1/chat/completions", hint: RequestProfileHint{IsStream: true}, expected: RequestTypeChatShortStream},
		{name: "tool stream", path: "/v1/chat/completions", hint: RequestProfileHint{IsStream: true, HasTools: true}, expected: RequestTypeToolCallStream},
		{name: "responses continuation", path: "/v1/responses", hint: RequestProfileHint{IsStream: true, HasUpstreamState: true}, expected: RequestTypeChatLongStream},
		{name: "image", path: "/v1/images/generations", expected: RequestTypeImageNonStream},
		{name: "embedding", path: "/v1/embeddings", expected: RequestTypeOther},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(http.MethodPost, test.path, nil)
			profile := InitializeRequestProfile(context, "gpt-5.6-sol", test.path, test.hint)
			require.Equal(t, test.expected, profile.RequestType)
			require.Equal(t, test.expected, RequestTypeFromContext(context))
		})
	}
}

func TestRefineRequestProfileUsesPromptAndConversationState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	InitializeRequestProfile(context, "gpt-5.6-sol", context.Request.URL.Path, RequestProfileHint{IsStream: true})
	stream := true
	request := &dto.OpenAIResponsesRequest{
		Stream:             &stream,
		PreviousResponseID: "response-1",
		Tools:              []byte(`[{"type":"function"}]`),
	}

	profile := RefineRequestProfile(context, types.RelayFormatOpenAIResponses, request, LongContextPromptTokenThreshold)

	require.Equal(t, RequestTypeToolCallStream, profile.RequestType)
	require.Equal(t, PromptSizeLarge, profile.PromptSizeBucket)
	require.Equal(t, MigrationUpstreamStateBound, profile.MigrationCapability)
}
