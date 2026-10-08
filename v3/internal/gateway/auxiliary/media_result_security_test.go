package auxiliary

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestAliMediaDecoderRejectsPrivateResultWithoutReadingIt(t *testing.T) {
	var reads atomic.Int32
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reads.Add(1)
		_, _ = w.Write([]byte("PRIVATE_INTERNAL_BYTES"))
	}))
	defer private.Close()
	a := &mediaAdapter{provider: "ali"}
	request := &gateway.Request{Model: "qwen-image", Body: []byte(`{"response_format":"b64_json"}`)}
	response := mediaTestResponse(`{"output":{"results":[{"url":"` + private.URL + `/private"}]}}`)
	out, err := a.Decode(context.Background(), request, gateway.Target{BaseURL: "https://dashscope.aliyuncs.com"}, Input{Operation: Images}, response)
	if err == nil || reads.Load() != 0 || strings.Contains(string(out.Body), "PRIVATE_INTERNAL_BYTES") {
		t.Fatalf("private provider result fetched: err=%v reads=%d body=%s", err, reads.Load(), out.Body)
	}
}
