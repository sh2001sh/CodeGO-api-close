package live

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
	"github.com/tidwall/gjson"
)

func TestContinuationPinsOriginalCredentialAndRewritesBackgroundID(t *testing.T) {
	ctx := context.Background()
	jobs := newBackgroundJobsMemory()
	job := BackgroundJob{ID: "resp_bg_previous", UserID: 1, KeyID: 11, ChannelID: 2, CredentialID: 20, UpstreamID: "resp_native", Status: "completed"}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	targets := []gateway.Target{{ChannelID: 1, CredentialID: 10, Provider: "openai", BaseURL: "https://wrong.invalid"}, {ChannelID: 2, CredentialID: 20, Provider: "openai", BaseURL: "https://correct.invalid", Secret: "upstream"}}
	h := &Handler{cfg: Config{Planner: livePlan{targets: targets}, BackgroundJobs: jobs, Repository: &liveRepo{items: map[string]Locator{}}, LocatorTTL: time.Hour}}
	req := &gateway.Request{ID: "next", Protocol: gateway.ProtocolResponses, Model: "gpt", Principal: gateway.Principal{UserID: 1, KeyID: 11}, Body: []byte(`{"model":"gpt","input":"next","previous_response_id":"resp_bg_previous"}`)}
	plan, err := h.Plan(ctx, req)
	if err != nil || len(plan) != 1 || plan[0].CredentialID != 20 {
		t.Fatalf("continuation route=%+v %v", plan, err)
	}
	up, err := h.TrackingProvider(responses.Provider{}).BuildRequest(ctx, req, plan[0])
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(up.Body)
	_ = up.Body.Close()
	if gjson.GetBytes(data, "previous_response_id").Str != "resp_native" || gjson.GetBytes(req.Body, "previous_response_id").Str != "resp_bg_previous" {
		t.Fatalf("body was not rewritten on copy: upstream=%s original=%s", data, req.Body)
	}
	req.Principal.KeyID = 12
	if _, err := h.Plan(ctx, req); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign key continuation allowed: %v", err)
	}
	req.Principal.KeyID = 11
	h.cfg.Planner = livePlan{targets: targets[:1]}
	if _, err := h.Plan(ctx, req); err == nil {
		t.Fatal("continuation crossed to a different credential")
	}
}

func TestContinuationNativeResponseIsOwnerScoped(t *testing.T) {
	repo := &liveRepo{items: map[string]Locator{"resp_private": {ID: "resp_private", UserID: 1, KeyID: 11, ChannelID: 2, CredentialID: 20}}}
	h := &Handler{cfg: Config{Planner: livePlan{targets: []gateway.Target{{ChannelID: 2, CredentialID: 20}}}, Repository: repo}}
	req := &gateway.Request{Protocol: gateway.ProtocolResponses, Principal: gateway.Principal{UserID: 2, KeyID: 11}, Body: []byte(`{"previous_response_id":"resp_private"}`)}
	if _, err := h.Plan(context.Background(), req); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign response accepted: %v", err)
	}
	req.Principal.UserID = 1
	plan, err := h.Plan(context.Background(), req)
	if err != nil || len(plan) != 1 {
		t.Fatalf("own response=%+v %v", plan, err)
	}
}
