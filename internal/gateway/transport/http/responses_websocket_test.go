package http

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestResponsesWebsocketPrewarmOverConnection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/v1/responses", ResponsesWebsocket)
	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/responses"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-5.6","generate":false,"input":[]}`)))
	_, created, err := conn.ReadMessage()
	require.NoError(t, err)
	_, completed, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(created), `"type":"response.created"`)
	require.Contains(t, string(completed), `"type":"response.completed"`)
}

func TestResponsesWebsocketStateMergesCachedContinuation(t *testing.T) {
	state := &responsesWebsocketState{}
	first, err := state.normalize([]byte(`{"type":"response.create","model":"gpt-5.6","store":false,"input":"hello"}`))
	require.NoError(t, err)
	state.complete(first, "resp_1", []byte(`[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}]`))

	second, err := state.normalize([]byte(`{"type":"response.create","previous_response_id":"resp_1","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`))
	require.NoError(t, err)

	var request map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(second.body, &request))
	require.Equal(t, "gpt-5.6", rawJSONString(request["model"]))
	require.NotContains(t, request, "previous_response_id")
	var input []json.RawMessage
	require.NoError(t, json.Unmarshal(request["input"], &input))
	require.Len(t, input, 3)
}

func TestResponsesWebsocketStatePassesPersistedContinuationThrough(t *testing.T) {
	state := &responsesWebsocketState{}
	turn, err := state.normalize([]byte(`{"type":"response.create","model":"gpt-5.6","store":true,"previous_response_id":"resp_persisted","input":[]}`))
	require.NoError(t, err)
	require.False(t, turn.canMerge)
	require.JSONEq(t, `{"model":"gpt-5.6","store":true,"previous_response_id":"resp_persisted","input":[],"stream":true}`, string(turn.body))
}

func TestResponsesWebsocketStateEvictsFailedCachedContinuation(t *testing.T) {
	state := &responsesWebsocketState{
		lastRequest: []byte(`{"model":"gpt-5.6","input":[],"stream":true}`),
		lastOutput:  []byte(`[]`), lastResponseID: "resp_1", canMerge: true,
	}
	turn, err := state.normalize([]byte(`{"type":"response.append","input":[]}`))
	require.NoError(t, err)
	state.fail(turn)
	require.Empty(t, state.lastResponseID)
	require.Empty(t, state.lastRequest)
}

func TestResponsesWebsocketAppendRequiresConnectionState(t *testing.T) {
	state := &responsesWebsocketState{}
	_, err := state.normalize([]byte(`{"type":"response.append","model":"gpt-5.6","input":[]}`))
	require.Error(t, err)
	status, code, _, param := responsesWebsocketErrorDetails(err)
	require.Equal(t, 400, status)
	require.Equal(t, "previous_response_not_found", code)
	require.Equal(t, "previous_response_id", param)
}

func TestResponsesWebsocketPrewarmCreatesChainableResponse(t *testing.T) {
	state := &responsesWebsocketState{}
	turn, err := state.normalize([]byte(`{"type":"response.create","model":"gpt-5.6","generate":false,"input":[]}`))
	require.NoError(t, err)
	require.Len(t, turn.prewarmPayloads, 2)
	require.Contains(t, string(turn.prewarmPayloads[0]), `"type":"response.created"`)
	require.Contains(t, string(turn.prewarmPayloads[1]), `"type":"response.completed"`)
	require.NotContains(t, string(turn.body), "generate")
}

func TestParseResponsesWebsocketSSEEvent(t *testing.T) {
	payload := parseResponsesWebsocketSSEEvent([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\"}}"))
	require.JSONEq(t, `{"type":"response.completed","response":{"id":"resp_1"}}`, string(payload))
	require.Nil(t, parseResponsesWebsocketSSEEvent([]byte("data: [DONE]")))
}

func TestBuildResponsesWebsocketHTTPError(t *testing.T) {
	payload := buildResponsesWebsocketHTTPError(400, []byte(`{"error":{"type":"invalid_request_error","code":"bad","message":"invalid"}}`))
	require.JSONEq(t, `{"type":"error","status":400,"error":{"type":"invalid_request_error","code":"bad","message":"invalid"}}`, string(payload))
}

func TestResponsesWebsocketWriterRestoresCompletedOutput(t *testing.T) {
	writer := newResponsesWebsocketHTTPWriter(nil, nil)
	writer.observePayloadLocked([]byte(`{"type":"response.output_item.done","output_index":1,"item":{"type":"message","id":"msg_2"}}`))
	writer.observePayloadLocked([]byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"}}`))
	writer.observePayloadLocked([]byte(`{"type":"response.completed","response":{"id":"resp_1","output":[]}}`))

	responseID, output, completed := writer.Completion()
	require.True(t, completed)
	require.Equal(t, "resp_1", responseID)
	require.JSONEq(t, `[{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"},{"type":"message","id":"msg_2"}]`, string(output))
}
