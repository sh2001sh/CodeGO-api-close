package runtime

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOutboundHTTPTraceCapturesNetworkStagesAndConnectionReuse(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		time.Sleep(10 * time.Millisecond)
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	client := server.Client()
	first := executeTracedRequest(t, client, server.URL, "first request")
	second := executeTracedRequest(t, client, server.URL, "second request")

	require.Equal(t, int64(0), first["outbound_conn_reused"])
	require.Equal(t, int64(1), second["outbound_conn_reused"])
	require.Equal(t, int64(2), first["outbound_http_proto_major"])
	require.Equal(t, int64(len("first request")), first["outbound_request_body_bytes"])
	require.Contains(t, first, "outbound_connect_ms")
	require.Contains(t, first, "outbound_tls_ms")
	require.Contains(t, first, "outbound_request_write_ms")
	require.GreaterOrEqual(t, first["upstream_server_wait_ms"], int64(8))
	require.Contains(t, first, "outbound_response_header_decode_ms")
}

func TestOutboundHTTPTraceOnlyAttachesToFirstRouteAttempt(t *testing.T) {
	trace := NewFirstByteTrace(time.Now())
	first, err := http.NewRequest(http.MethodPost, "https://first.example", strings.NewReader("first"))
	require.NoError(t, err)
	second, err := http.NewRequest(http.MethodPost, "https://second.example", strings.NewReader("second"))
	require.NoError(t, err)

	firstContext := first.Context()
	first = WithOutboundHTTPTrace(first, trace, int64(len("first")))
	secondContext := second.Context()
	second = WithOutboundHTTPTrace(second, trace, int64(len("second")))

	require.NotEqual(t, firstContext, first.Context())
	require.Equal(t, secondContext, second.Context())
	require.Equal(t, int64(len("first")), trace.ProgressSnapshot(time.Now())["outbound_request_body_bytes"])
}

func executeTracedRequest(t *testing.T, client *http.Client, target, body string) map[string]int64 {
	t.Helper()
	trace := NewFirstByteTrace(time.Now())
	trace.MarkUpstreamStart()
	trace.MarkUpstreamRequestReady()

	request, err := http.NewRequest(http.MethodPost, target, strings.NewReader(body))
	require.NoError(t, err)
	request = WithOutboundHTTPTrace(request, trace, int64(len(body)))
	response, err := client.Do(request)
	require.NoError(t, err)
	trace.MarkOutboundHTTPVersion(response.ProtoMajor, response.ProtoMinor)
	trace.MarkUpstreamResponseHeaders()
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	return trace.ProgressSnapshot(time.Now())
}
