package live

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

func TestTrackingRawRequestPreparesActualFilesContinuationAndLocator(t *testing.T) {
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 11, []byte("private content"))
	var uploads atomic.Int64
	seen := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/files" {
			uploads.Add(1)
			_, _ = io.WriteString(w, `{"id":"file-upstream-raw"}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		seen <- body
		testResponseSSE(w, "resp_raw_current")
	}))
	defer upstream.Close()
	repo := &attachmentsRepository{}
	h := attachmentsHandler(t, store, repo)
	h.cfg.FinalizeTimeout = time.Second
	h.cfg.BackgroundJobs = newBackgroundJobsMemory()
	previous := BackgroundJob{ID: "resp_bg_raw_previous", UserID: 11, KeyID: 101, ChannelID: 7, CredentialID: 9, Status: "completed", UpstreamID: "resp_raw_previous"}
	if err := h.cfg.BackgroundJobs.Create(context.Background(), previous); err != nil {
		t.Fatal(err)
	}
	request := attachmentsRequest(file.ID, gateway.ProtocolResponses)
	request.Stream = true
	request.Body = []byte(fmt.Sprintf(`{"model":"wire-model","stream":true,"input":[{"type":"input_file","file_id":%q}],"previous_response_id":"resp_bg_raw_previous","unknown":{"keep":9007199254740993}}`, file.ID))
	original := string(request.Body)
	target := attachmentsTarget(upstream.URL)
	target.UpstreamModel = "must-not-replace-wire-model"
	target.Settings = map[string]any{"pass_through_body_enabled": true}
	provider := h.TrackingProvider(responses.Provider{})
	out, err := gateway.BuildProviderRequest(context.Background(), provider, request, target)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(out)
	if err != nil {
		t.Fatal(err)
	}
	stream := provider.Decode(request, response)
	defer func() { _ = stream.Close() }()
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	wire := <-seen
	if uploads.Load() != 1 || gjson.GetBytes(wire, "input.0.file_id").Str != "file-upstream-raw" || gjson.GetBytes(wire, "previous_response_id").Str != "resp_raw_previous" {
		t.Fatalf("actual references not prepared uploads=%d wire=%s", uploads.Load(), wire)
	}
	if string(request.Body) != original || gjson.GetBytes(wire, "model").Str != "wire-model" || gjson.GetBytes(wire, "unknown.keep").Raw != "9007199254740993" || strings.Contains(string(wire), "endpoint template") {
		t.Fatalf("raw body converted or mutated original=%s wire=%s", request.Body, wire)
	}
	locator, err := repo.Get(context.Background(), "resp_raw_current", 11, 101)
	if err != nil || locator.ChannelID != 7 || locator.CredentialID != 9 {
		t.Fatalf("raw builder lost locator context: %+v %v", locator, err)
	}
}

func TestResponsesRawSocketForeignFileRefundsWithoutUpload(t *testing.T) {
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 2, []byte("another owner"))
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer upstream.Close()
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 1)}
	target := gateway.Target{ChannelID: 1, CredentialID: 7, Provider: "openai", BaseURL: upstream.URL, Secret: "upstream", Settings: map[string]any{"pass_through_body_enabled": true}}
	h, server, _ := socketFixture(t, []gateway.Target{target}, ledger)
	h.cfg.Files = store
	h.cfg.Providers["openai"] = h.TrackingProvider(responses.Provider{})
	conn := dialSocket(t, server.URL, "/responses")
	_ = websocket.Message.Send(conn, fmt.Sprintf(`{"type":"response.create","model":"gpt-test","input":[{"type":"input_file","file_id":%q}]}`, file.ID))
	var message wireFrame
	if err := frameCodec.Receive(conn, &message); err != nil {
		t.Fatal(err)
	}
	out := <-ledger.finalized
	if gjson.GetBytes(message.data, "type").Str != "error" || out.Charge || ledger.reserves.Load() != 1 || calls.Load() != 0 {
		t.Fatalf("foreign raw file not rejected/refunded message=%s out=%+v calls=%d", message.data, out, calls.Load())
	}
}

func TestRawBackgroundPreservesOpaqueWireAndRefundsForeignFiles(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(fmt.Sprint(foreign), func(t *testing.T) {
			var calls atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				body, _ := io.ReadAll(r.Body)
				if gjson.GetBytes(body, "opaque.keep").Str != "raw" || strings.Contains(string(body), "endpoint template") {
					t.Errorf("background raw body lost: %s", body)
				}
				_, _ = io.WriteString(w, backgroundCompletedSnapshot)
			}))
			defer upstream.Close()
			h, _, wrapped, repo, billing := backgroundJobsFixture(t, upstream.URL, "openai")
			target, _ := h.cfg.Resolve(context.Background(), 7, 9)
			target.Settings = map[string]any{"pass_through_body_enabled": true}
			h.cfg.Planner = backgroundJobsPlanner{target: target}
			h.cfg.Resolve = func(context.Context, int64, int64) (gateway.Target, error) { return target, nil }
			body := `{"model":"gpt-test","background":true,"opaque":{"keep":"raw"}}`
			if foreign {
				store := filesTestStore(t)
				file := filesTestCreate(t, store, 2, []byte("foreign"))
				h.cfg.Files = store
				body = fmt.Sprintf(`{"model":"gpt-test","background":true,"input":[{"type":"input_file","file_id":%q}],"opaque":{"keep":"raw"}}`, file.ID)
			}
			id := createBackgroundForTest(t, wrapped, "/responses", body)
			if err := h.Reconcile(context.Background(), 1); err != nil {
				t.Fatal(err)
			}
			job, err := repo.GetOwned(context.Background(), id, 1, 11)
			out, count, _ := billing.result(id)
			wantCalls := int64(1)
			if foreign {
				wantCalls = 0
			}
			if err != nil || !job.Billed || count != 1 || out.Charge == foreign || calls.Load() != wantCalls {
				t.Fatalf("background raw outcome foreign=%v job=%+v out=%+v calls=%d", foreign, job, out, calls.Load())
			}
		})
	}
}

func TestTrackingRawContinuationRejectsForeignOwnerAndWrongRoute(t *testing.T) {
	for _, foreign := range []bool{true, false} {
		t.Run(fmt.Sprint(foreign), func(t *testing.T) {
			jobs := newBackgroundJobsMemory()
			job := BackgroundJob{ID: "resp_bg_bound", UserID: 11, KeyID: 101, ChannelID: 7, CredentialID: 9, Status: "completed", UpstreamID: "resp_previous"}
			target := attachmentsTarget("https://unused.invalid")
			target.Settings = map[string]any{"pass_through_body_enabled": true}
			status := 502
			if foreign {
				job.UserID, status = 22, 404
			} else {
				target.ChannelID = 8
			}
			if err := jobs.Create(context.Background(), job); err != nil {
				t.Fatal(err)
			}
			h := &Handler{cfg: Config{BackgroundJobs: jobs}}
			req := &gateway.Request{Protocol: gateway.ProtocolResponses, Model: "gpt", Principal: gateway.Principal{UserID: 11, KeyID: 101}, Body: []byte(`{"previous_response_id":"resp_bg_bound","input":"next"}`)}
			_, err := gateway.BuildProviderRequest(context.Background(), h.TrackingProvider(responses.Provider{}), req, target)
			var failure *gateway.UpstreamError
			if !errors.As(err, &failure) || failure.Status != status || gjson.GetBytes(req.Body, "previous_response_id").Str != "resp_bg_bound" {
				t.Fatalf("raw continuation escaped owner/route binding err=%v body=%s", err, req.Body)
			}
		})
	}
}
