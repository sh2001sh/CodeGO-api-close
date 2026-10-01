package live

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
	"github.com/tidwall/gjson"
)

type backgroundFinalizingProvider struct {
	responses.Provider
	finalize func(*http.Request) error
}

func (provider backgroundFinalizingProvider) FinalizeRequest(_ context.Context, out *http.Request, _ *gateway.Request, _ gateway.Target) error {
	return provider.finalize(out)
}

func TestBackgroundJobsLocalFinalizationSignsOverriddenBytesBeforeDispatch(t *testing.T) {
	for _, reject := range []bool{false, true} {
		h, _, wrapped, repo, billing := backgroundJobsFixture(t, "https://unused.invalid", "signed-local")
		target, _ := h.cfg.Resolve(context.Background(), 7, 9)
		target.ParamOverride = map[string]any{"temperature": 0.25}
		target.HeaderOverride = map[string]string{"X-Route": "configured"}
		h.cfg.Resolve = func(context.Context, int64, int64) (gateway.Target, error) { return target, nil }
		var finalized, forwarded atomic.Int64
		h.cfg.Providers["signed-local"] = backgroundFinalizingProvider{finalize: func(out *http.Request) error {
			finalized.Add(1)
			copyBody, err := out.GetBody()
			if err != nil {
				return err
			}
			body, err := io.ReadAll(copyBody)
			_ = copyBody.Close()
			if err != nil {
				return err
			}
			if gjson.GetBytes(body, "temperature").Float() != 0.25 || gjson.GetBytes(body, "background").Exists() || out.Header.Get("X-Route") != "configured" {
				t.Errorf("finalizer ran before conversion/overrides: %s", body)
			}
			if reject {
				return errors.New("signing rejected")
			}
			out.Header.Set("X-Finalized", "signed-final-bytes")
			return nil
		}}
		h.cfg.Clients = func(context.Context, gateway.Target) (*http.Client, error) {
			return &http.Client{Transport: backgroundOverrideTransport(func(req *http.Request) (*http.Response, error) {
				forwarded.Add(1)
				if req.Header.Get("X-Finalized") != "signed-final-bytes" {
					t.Error("request dispatched before signing")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(backgroundLocalEvents)), Request: req}, nil
			})}, nil
		}
		const original = `{"model":"gpt-test","background":true,"temperature":1}`
		id := createBackgroundForTest(t, wrapped, "/responses", original)
		if err := h.Reconcile(context.Background(), 10); err != nil {
			t.Fatal(err)
		}
		job, _ := repo.GetOwned(context.Background(), id, 1, 11)
		out, count, _ := billing.result(id)
		wantCalls, wantStatus := int64(1), "completed"
		if reject {
			wantCalls, wantStatus = 0, "failed"
		}
		if finalized.Load() != 1 || forwarded.Load() != wantCalls || job.Status != wantStatus || !job.Billed || out.Charge == reject || count != 1 || string(job.Body) != original {
			t.Fatalf("finalization reject=%v job=%+v out=%+v finalized=%d forwarded=%d", reject, job, out, finalized.Load(), forwarded.Load())
		}
	}
}
