package workflow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestWorkflowRoutesRequireAuthorization(t *testing.T) {
	for _, route := range []struct{ method, path, body string }{
		{"POST", "/v1/videos", `{"model":"video"}`},
		{"GET", "/v1/videos/id", ""}, {"GET", "/v1/videos/id/content", ""},
		{"POST", "/v1/videos/id/remix", `{"model":"video"}`},
		{"POST", "/suno/fetch", `{"ids":["id"]}`},
	} {
		for _, auth := range []struct {
			key    string
			status int
		}{{"", 401}, {"invalid", 401}, {"unavailable", 503}} {
			t.Run(route.path+"/"+auth.key, func(t *testing.T) {
				f := newAPIFixture(t)
				w := f.request(route.method, route.path, auth.key, route.body, "")
				if w.Code != auth.status {
					t.Fatalf("authorization = %d, want %d: %s", w.Code, auth.status, w.Body.String())
				}
				if f.provider.submits.Load()+f.provider.polls.Load()+f.provider.contents.Load() != 0 || len(f.settler.reserved) != 0 {
					t.Fatal("unauthorized request reached provider or reserved funds")
				}
			})
		}
	}
}

func TestWorkflowOwnershipIsolation(t *testing.T) {
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/v1/videos/video", ""}, {"GET", "/v1/videos/video/content", ""},
		{"POST", "/v1/videos/video/remix", `{"prompt":"change"}`},
		{"GET", "/suno/fetch/music", ""},
	} {
		t.Run(route.path, func(t *testing.T) {
			f := newAPIFixture(t)
			f.repo.seed(ownedTask("video", "openai_video"))
			f.repo.seed(ownedTask("music", "suno"))
			w := f.request(route.method, route.path, "other", route.body, "")
			if w.Code != 404 {
				t.Fatalf("foreign task = %d %s", w.Code, w.Body.String())
			}
			if f.provider.submits.Load()+f.provider.polls.Load()+f.provider.contents.Load() != 0 || len(f.settler.reserved) != 0 {
				t.Fatal("foreign task reached provider or billing")
			}
		})
	}
	f := newAPIFixture(t)
	mine, other := ownedTask("mine", "suno"), ownedTask("foreign", "suno")
	other.UserID = 22
	f.repo.seed(mine)
	f.repo.seed(other)
	w := f.request("POST", "/suno/fetch", "owner", `{"ids":["mine","foreign","missing"]}`, "")
	var result struct {
		Data []struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Data) != 1 || result.Data[0].TaskID != "mine" {
		t.Fatalf("batch leaked or omitted task: %d %s", w.Code, w.Body.String())
	}
}

func TestWorkflowAcceptedTaskIsDurableBeforeResponse(t *testing.T) {
	f := newAPIFixture(t)
	f.provider.submitFn = func(_ context.Context, target gateway.Target, in native.Submit) (native.Result, error) {
		stored := f.repo.one(t)
		if stored.Status != "submitting" || stored.CostState != "reserved" || stored.LeaseID == "" || stored.UpstreamID != "" {
			t.Fatalf("upstream invoked before durable reservation: %+v", stored)
		}
		if stored.ID != f.settler.reserved[0] || stored.Reservation.EstimatedCredits != 90 || len(stored.Reservation.Data) == 0 {
			t.Fatal("durable task lost billing identity or reservation")
		}
		if target.CredentialID != stored.CredentialID || in.Model != "video" {
			t.Fatal("wrong route or model")
		}
		if strings.Contains(string(stored.Body)+string(stored.Reservation.Data), target.Secret) {
			t.Fatal("credential persisted")
		}
		return native.Result{ID: "accepted-native", Status: "queued"}, nil
	}
	w := f.request("POST", "/v1/videos", "owner", `{"model":"video"}`, "")
	stored := f.repo.one(t)
	var result struct {
		ID string `json:"id"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.ID != stored.ID || w.Header().Get("X-Task-Id") != stored.ID {
		t.Fatalf("accepted response lost durable ID: %d %s", w.Code, w.Body.String())
	}
	if stored.UpstreamID != "accepted-native" || stored.Status != "queued" || stored.LeaseID != "" || len(f.settler.calls) != 0 {
		t.Fatalf("success before acceptance persisted: %+v", stored)
	}
}

func TestWorkflowSubmissionFailureKeepsOrRefundsReservation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		result      native.Result
		err         error
		state, cost string
		calls       int
	}{
		{"rejected", native.Result{}, &native.Rejected{Status: 400}, "failed", "refunded", 1},
		{"invalid", native.Result{}, &native.InvalidRequest{Err: errors.New("bad image")}, "failed", "refunded", 1},
		{"provider_failed", native.Result{Status: "failed", Error: "rejected"}, nil, "failed", "refunded", 1},
		{"transport", native.Result{}, errors.New("timeout"), "submission_unknown", "reserved", 0},
		{"malformed_acceptance", native.Result{Status: "queued"}, nil, "submission_unknown", "reserved", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAPIFixture(t)
			f.provider.submitFn = func(context.Context, gateway.Target, native.Submit) (native.Result, error) { return tc.result, tc.err }
			w := f.request("POST", "/v1/videos", "owner", `{"model":"video"}`, "")
			stored := f.repo.one(t)
			if w.Code != 502 || stored.Status != tc.state || stored.CostState != tc.cost || len(f.settler.calls) != tc.calls {
				t.Fatalf("failure disposition = %d status=%s cost=%s finalizations=%d", w.Code, stored.Status, stored.CostState, len(f.settler.calls))
			}
			if tc.calls == 1 && (f.settler.calls[0].result.Status != "failed" || f.settler.calls[0].id != stored.ID) {
				t.Fatal("refund lost task identity")
			}
			if count, err := f.handler.Reconcile(context.Background(), 10); err != nil || count != 0 || f.provider.polls.Load() != 0 {
				t.Fatalf("terminal or ambiguous submission polled: count=%d err=%v", count, err)
			}
		})
	}
}

func TestWorkflowStorageFailureCannotAcknowledgeAcceptance(t *testing.T) {
	f := newAPIFixture(t)
	f.repo.saveFailures = 1
	w := f.request("POST", "/v1/videos", "owner", `{"model":"video"}`, "")
	stored := f.repo.one(t)
	if w.Code != 503 || stored.Status != "submitting" || stored.CostState != "reserved" || len(f.settler.calls) != 0 {
		t.Fatalf("unpersisted acceptance acknowledged or refunded: %d task=%+v", w.Code, stored)
	}
	if _, err := f.handler.Reconcile(context.Background(), 10); err != nil || f.provider.polls.Load() != 0 {
		t.Fatal("ambiguous submission blindly polled")
	}
	f = newAPIFixture(t)
	f.repo.createErr = errors.New("disk unavailable")
	w = f.request("POST", "/v1/videos", "owner", `{"model":"video"}`, "")
	if w.Code != 503 || f.provider.submits.Load() != 0 || len(f.settler.calls) != 1 || f.settler.calls[0].result.Status != "failed" {
		t.Fatal("failed durable create reached upstream or failed compensation")
	}
}

func TestWorkflowBodyBoundariesAndMultipart(t *testing.T) {
	for _, tc := range []struct{ name, body, contentType string }{
		{"malformed", "{", ""}, {"null", "null", ""}, {"array", "[]", ""}, {"missing_model", "{}", ""},
		{"too_large", `{"model":"video","prompt":"` + strings.Repeat("x", 100) + `"}`, ""},
		{"broken_multipart", "not-multipart", "multipart/form-data; boundary=boundary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAPIFixture(t, func(c *workflow.Config) { c.MaxBodyBytes = 64 })
			w := f.request("POST", "/v1/videos", "owner", tc.body, tc.contentType)
			if w.Code != 400 || f.provider.submits.Load() != 0 || len(f.settler.reserved) != 0 {
				t.Fatalf("invalid body = %d %s", w.Code, w.Body.String())
			}
		})
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", "video"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("input_reference", "seed.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write([]byte("image-reference")); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	f := newAPIFixture(t)
	f.provider.submitFn = func(_ context.Context, _ gateway.Target, in native.Submit) (native.Result, error) {
		if in.Model != "video" || !bytes.Equal(in.Body, body.Bytes()) || in.ContentType != writer.FormDataContentType() {
			t.Fatal("multipart altered before native provider")
		}
		return native.Result{ID: "multipart-accepted", Status: "queued"}, nil
	}
	w := f.request("POST", "/v1/videos", "owner", body.String(), writer.FormDataContentType())
	if w.Code != 200 {
		t.Fatalf("multipart = %d %s", w.Code, w.Body.String())
	}
	stored := f.repo.one(t)
	if !json.Valid(stored.Body) || bytes.Contains(stored.Body, []byte("image-reference")) {
		t.Fatal("billing body retained uploaded file")
	}
}

func TestWorkflowOwnedContentAndRemix(t *testing.T) {
	f := newAPIFixture(t)
	origin := ownedTask("origin", "openai_video")
	f.repo.seed(origin)
	w := f.request("GET", "/v1/videos/origin/content", "owner", "", "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "video/mp4" || w.Body.String() != "video-content" {
		t.Fatalf("content = %d %s", w.Code, w.Body.String())
	}
	f.provider.submitFn = func(_ context.Context, _ gateway.Target, in native.Submit) (native.Result, error) {
		if in.OriginID != origin.UpstreamID || in.Action != "remix" || in.Model != origin.Model {
			t.Fatal("remix lost native origin or inherited model")
		}
		return native.Result{ID: "remixed", Status: "queued"}, nil
	}
	w = f.request("POST", "/v1/videos/origin/remix", "owner", `{"prompt":"blue"}`, "")
	if w.Code != 200 || f.provider.submits.Load() != 1 {
		t.Fatalf("remix = %d %s", w.Code, w.Body.String())
	}
}

func TestWorkflowContentSessionOwnershipAndBatchBoundaries(t *testing.T) {
	for _, user := range []int64{11, 22} {
		f := newAPIFixture(t, func(c *workflow.Config) {
			c.ContentAuthorizer = func(*http.Request) (gateway.Principal, error) { return gateway.Principal{UserID: user}, nil }
		})
		f.repo.seed(ownedTask("owned", "openai_video"))
		w := f.request("GET", "/v1/videos/owned/content", "", "", "")
		want := 200
		if user != 11 {
			want = 404
		}
		if w.Code != want {
			t.Fatalf("session user %d content = %d, want %d", user, w.Code, want)
		}
		w = f.request("GET", "/v1/videos/owned", "", "", "")
		if w.Code != 401 {
			t.Fatal("content session granted API fetch access")
		}
		w = f.request("GET", "/v1/videos/owned/content", "invalid", "", "")
		if w.Code != 401 {
			t.Fatal("invalid explicit API key fell back to user session")
		}
	}
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = "task"
	}
	tooMany, err := json.Marshal(map[string]any{"ids": ids})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"{", string(tooMany), `{"ids":["` + strings.Repeat("x", 1<<20) + `"]}`} {
		f := newAPIFixture(t)
		w := f.request("POST", "/suno/fetch", "owner", body, "")
		if w.Code != 400 {
			t.Fatalf("invalid batch = %d", w.Code)
		}
	}
}
