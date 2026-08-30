package stream

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type countingResponseWriter struct {
	http.ResponseWriter
	writes int
}

func (w *countingResponseWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.ResponseWriter.Write(p)
}

func (w *countingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *countingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (w *countingResponseWriter) CloseNotify() <-chan bool {
	return make(chan bool)
}

func (w *countingResponseWriter) Pusher() http.Pusher { return nil }

var _ io.Writer = (*countingResponseWriter)(nil)

func TestWriteSSEPartsPreservesEventFraming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/", nil)

	err := writeSSEParts(ctx, "event: response.output_text.delta\n", "data: {\"text\":\"hello\"}")
	require.NoError(t, err)
	require.Equal(t, "event: response.output_text.delta\ndata: {\"text\":\"hello\"}\n\n", recorder.Body.String())
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
}

func TestWriteSSEPartsKeepsExistingTrailingNewlineBehavior(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/", nil)

	err := writeSSEParts(ctx, "event: message\n", "data: payload\n")
	require.NoError(t, err)
	require.Equal(t, "event: message\ndata: payload\n\n\n", recorder.Body.String())
}

func TestWriteSSEPartsUsesOneUnderlyingWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	underlying := &countingResponseWriter{ResponseWriter: httptest.NewRecorder()}
	ctx, _ := gin.CreateTestContext(underlying)
	ctx.Request = httptest.NewRequest("GET", "/", nil)

	require.NoError(t, writeSSEParts(ctx, "event: message\n", "data: payload"))
	require.Equal(t, 1, underlying.writes)
}

func renderSSEPartsBaseline(c *gin.Context, event, data string) {
	c.Render(-1, CustomEvent{Data: event})
	c.Render(-1, CustomEvent{Data: data})
	_ = FlushWriter(c)
}

func TestWriteSSEPartsMatchesBaselineOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	baselineRecorder := httptest.NewRecorder()
	baselineContext, _ := gin.CreateTestContext(baselineRecorder)
	baselineContext.Request = httptest.NewRequest("GET", "/", nil)
	renderSSEPartsBaseline(baselineContext, "event: message\n", "data: payload")

	optimizedRecorder := httptest.NewRecorder()
	optimizedContext, _ := gin.CreateTestContext(optimizedRecorder)
	optimizedContext.Request = httptest.NewRequest("GET", "/", nil)
	require.NoError(t, writeSSEParts(optimizedContext, "event: message\n", "data: payload"))
	require.Equal(t, baselineRecorder.Body.String(), optimizedRecorder.Body.String())
}

func BenchmarkSSEWriteAB(b *testing.B) {
	gin.SetMode(gin.TestMode)
	event := "event: response.output_text.delta\n"
	data := "data: {\"delta\":\"hello\"}"
	b.Run("A_two_render_writes", func(b *testing.B) {
		recorder := httptest.NewRecorder()
		counting := &countingResponseWriter{ResponseWriter: recorder}
		ctx, _ := gin.CreateTestContext(counting)
		ctx.Request = httptest.NewRequest("GET", "/", nil)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			recorder.Body.Reset()
			renderSSEPartsBaseline(ctx, event, data)
		}
		b.ReportMetric(float64(counting.writes)/float64(b.N), "writes/op")
	})
	b.Run("B_single_write", func(b *testing.B) {
		recorder := httptest.NewRecorder()
		counting := &countingResponseWriter{ResponseWriter: recorder}
		ctx, _ := gin.CreateTestContext(counting)
		ctx.Request = httptest.NewRequest("GET", "/", nil)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			recorder.Body.Reset()
			if err := writeSSEParts(ctx, event, data); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(counting.writes)/float64(b.N), "writes/op")
	})
}
