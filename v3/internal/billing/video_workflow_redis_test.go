//go:build pgintegration

package billing

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/gemini"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Actual native submit/poll against a local provider, durable WorkflowSettler
// restore and real Redis settlement. Provider seconds remain physical seconds.
func TestVeoWorkflowNativeAliasFrozenSettlementAndWAL(t *testing.T) {
	for _, row := range []struct {
		name, size, resolution string
		want                   int64
		wal                    bool
		missingDuration        bool
	}{
		{"720", "1280x720", "720p", 80_000_016, false, false},
		{"4k", "3840x2160", "4k", 186_666_678, false, false},
		{"4kWAL", "3840x2160", "4k", 186_666_678, true, false},
		{"4kSourceURIOnly", "3840x2160", "4k", 186_666_678, true, true},
	} {
		t.Run(row.name, func(t *testing.T) {
			s, rdb, _, _ := setup(t, 2_000_000_000)
			req, snapshot := veoPriceFixture(`{"prompt":"cat","model":"video-alias","seconds":8,"size":"`+row.size+`"}`, "veo-3.1-fast-generate-preview", 10_000_002)
			s.snapshot = func() *catalog.Snapshot { return snapshot }
			var err error
			if row.wal {
				s.wal, err = openWAL(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(s.Close)
			}
			reservation, err := NewWorkflowSettler(s).Reserve(ctx, req)
			if err != nil || reservation.EstimatedCredits != credits.Micro(row.want) {
				t.Fatalf("reserve=%+v %v", reservation, err)
			}
			if b, held := balance(t, rdb); b != 2_000_000_000 || held != row.want {
				t.Fatalf("admission=%d/%d", b, held)
			}
			const operation = "models/veo-3.1-fast-generate-preview/operations/task"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodPost:
					if r.URL.Path != "/v1beta/models/veo-3.1-fast-generate-preview:predictLongRunning" {
						t.Errorf("alias upstream=%s", r.URL.Path)
					}
					var body struct {
						Parameters map[string]any `json:"parameters"`
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil || body.Parameters["resolution"] != row.resolution || body.Parameters["durationSeconds"] != float64(8) {
						t.Errorf("actual outgoing parameters=%#v", body)
					}
					_, _ = io.WriteString(w, `{"name":"`+operation+`"}`)
				case http.MethodGet:
					duration := `,"durationSeconds":8`
					if row.missingDuration {
						duration = ""
					}
					_, _ = io.WriteString(w, `{"name":"`+operation+`","done":true,"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"https://video.test/movie"`+duration+`}}]}}}`)
				default:
					t.Errorf("unexpected request=%s", r.Method)
				}
			}))
			defer server.Close()
			target := req.Targets[0]
			target.BaseURL, target.Secret = server.URL, "fixture-key"
			provider := gemini.New(server.Client())
			submitted, err := provider.Submit(ctx, target, native.Submit{Model: req.Model, Body: req.Body})
			if err != nil || submitted.Status != "queued" {
				t.Fatalf("native submit=%+v %v", submitted, err)
			}
			result, err := provider.Poll(ctx, target, native.Task{UpstreamID: submitted.ID})
			physicalUnits := float64(8)
			if row.missingDuration {
				physicalUnits = 0
			}
			if err != nil || result.Status != "completed" || result.Units != physicalUnits {
				t.Fatalf("native poll=%+v %v", result, err)
			}
			// The task body identity stays immutable, but current route and catalog
			// facts change. Durable admitted pricing must win after restart.
			req.Reserve = nil
			req.Targets[0].UpstreamModel = "veo-3.1-generate-preview"
			snapshot.Prices[req.Model] = catalog.Price{Mode: "per_request", PerRequest: 999}
			restarted := NewWorkflowSettler(s)
			if row.wal {
				for range 3 {
					s.br.fail()
				}
				for range 2 {
					_, err = restarted.Finalize(ctx, req, reservation, result)
					if !errors.Is(err, gateway.ErrBillingUnavailable) {
						t.Fatalf("WAL marked task paid before replay=%v", err)
					}
				}
				if len(events(t, rdb)) != 0 {
					t.Fatal("WAL settled early")
				}
				s.snapshot = func() *catalog.Snapshot { return nil }
				for range 2 {
					if err := s.Replay(ctx); err != nil {
						t.Fatal(err)
					}
				}
			}
			for range 2 {
				actual, err := restarted.Finalize(ctx, req, reservation, result)
				if err != nil || actual != credits.Micro(row.want) {
					t.Fatalf("restored actual=%d %v want%d", actual, err, row.want)
				}
			}
			if b, held := balance(t, rdb); b != 2_000_000_000-row.want || held != 0 {
				t.Fatalf("settled=%d/%d", b, held)
			}
			if all := events(t, rdb); len(all) != 1 || all[0][FieldAmount] != strconv.FormatInt(row.want, 10) || all[0][FieldVideoDurationMicros] != "8000000" {
				t.Fatalf("settlement events=%#v", all)
			} else if row.missingDuration && all[0][FieldEstimated] != "1" {
				t.Fatal("URI-only duration was presented as measured usage")
			}
		})
	}
}
