package auxiliary

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestChannelOverridesAndSelectedClientPreservePricingInputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if gjson.GetBytes(body, "input").String() != "configured" || r.Header.Get("X-Channel") != "selected" || r.Header.Get("Authorization") != "Bearer upstream-secret" || r.ContentLength != int64(len(body)) {
			t.Errorf("body=%s headers=%v contentlength=%d", body, r.Header, r.ContentLength)
		}
		_, _ = io.WriteString(w, `{"data":[{"embedding":[1]}],"usage":{"prompt_tokens":1}}`)
	}))
	defer server.Close()
	h, plan, settle, _ := testHandler(t, server.URL)
	plan.targets[0].ParamOverride = map[string]any{"input": "configured"}
	plan.targets[0].HeaderOverride = map[string]string{"X-Channel": "selected"}
	plan.targets[0].Fingerprint.UserAgent = "credential-agent"
	calls := 0
	h.cfg.Clients = func(_ context.Context, target gateway.Target) (*http.Client, error) {
		calls++
		if target.Fingerprint.UserAgent != "credential-agent" {
			t.Error("credential identity lost")
		}
		return server.Client(), nil
	}
	original := `{"model":"alias","input":"original"}`
	w := invoke(h, "/v1/embeddings", original)
	if w.Code != 200 || calls != 1 || !settle.out.Charge || string(settle.req.Body) != original {
		t.Fatalf("status=%d calls=%d outcome=%+v body=%s", w.Code, calls, settle.out, settle.req.Body)
	}
	if !reflect.DeepEqual(plan.targets[0].ParamOverride, map[string]any{"input": "configured"}) {
		t.Fatal("target override mutated")
	}
}

func TestChannelStatusMappingControlsFailover(t *testing.T) {
	for _, test := range []struct {
		status   int
		mapping  map[string]int
		want     int
		attempts int
	}{
		{400, map[string]int{"400": 503}, 200, 2}, {500, map[string]int{"500": 400}, 400, 1},
	} {
		first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(test.status) }))
		second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"data":[{"embedding":[1]}]}`) }))
		h, plan, _, _ := testHandler(t, first.URL, second.URL)
		plan.targets[0].StatusCodeMapping = test.mapping
		w := invoke(h, "/v1/embeddings", `{"model":"alias","input":"hello"}`)
		first.Close()
		second.Close()
		if w.Code != test.want || len(plan.reports) != test.attempts {
			t.Fatalf("status=%d attempts=%d", w.Code, len(plan.reports))
		}
	}
}

func TestIntentionalChannelRejectionHonorsSkipRetry(t *testing.T) {
	for _, skip := range []bool{true, false} {
		h, plan, settle, _ := testHandler(t, "http://unused.invalid", "http://unused.invalid")
		for i := range plan.targets {
			plan.targets[i].ParamOverride = map[string]any{"operations": []any{map[string]any{"mode": "return_error", "value": map[string]any{"status_code": 503, "message": "rejected", "code": "policy_rejection", "skip_retry": skip}}}}
		}
		w := invoke(h, "/v1/embeddings", `{"model":"alias","input":"hello"}`)
		want := 2
		if skip {
			want = 1
		}
		if w.Code != 503 || len(plan.reports) != want || settle.out.Charge {
			t.Fatalf("skip=%v status=%d reports=%d outcome=%+v body=%s", skip, w.Code, len(plan.reports), settle.out, w.Body.String())
		}
	}
}

func TestJimengResignsAfterNativeOverrides(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if gjson.GetBytes(data, "prompt").String() != "configured prompt" {
			t.Errorf("prompt=%s", data)
		}
		check := httptest.NewRequest(r.Method, "http://"+r.Host+r.URL.RequestURI(), strings.NewReader(string(data)))
		check.Header = r.Header.Clone()
		check.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(string(data))), nil }
		date, err := time.Parse("20060102T150405Z", r.Header.Get("X-Date"))
		if err != nil {
			t.Error(err)
			return
		}
		if err = signJimengMedia(check, "access|secret", date); err != nil {
			t.Error(err)
			return
		}
		if check.Header.Get("Authorization") != r.Header.Get("Authorization") || check.Header.Get("X-Content-Sha256") != r.Header.Get("X-Content-Sha256") {
			t.Error("override invalidated native signature")
		}
		_, _ = io.WriteString(w, `{"code":10000,"data":{"binary_data_base64":["aW1hZ2U="]}}`)
	}))
	defer server.Close()
	h, plan, settle, _ := testHandler(t, server.URL)
	plan.targets[0].Provider, plan.targets[0].Secret = "jimeng", "access|secret"
	plan.targets[0].ParamOverride = map[string]any{"prompt": "configured prompt"}
	w := invoke(h, "/v1/images/generations", `{"model":"image","prompt":"original","response_format":"b64_json"}`)
	if w.Code != 200 || settle.out.Usage.ImageCount != 1 || !settle.out.Charge {
		t.Fatalf("status=%d outcome=%+v body=%s", w.Code, settle.out, w.Body.String())
	}
}
