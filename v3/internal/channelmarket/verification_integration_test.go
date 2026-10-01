//go:build pgintegration

package channelmarket_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestUnlistedModelCannotPublishOrBecomeVerifiedByAdminAnnotation(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	crypto, err := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s := channelmarket.New(f.pool, crypto, f.poster, channelmarket.Config{
		ListModels: func(context.Context, channelmarket.FetchModelsRequest) ([]string, error) {
			return []string{"different-model"}, nil
		},
		Probe: func(_ context.Context, p channelmarket.ProbeRequest) (channelmarket.ModelTest, error) {
			return channelmarket.ModelTest{Model: p.Model, Status: "passed"}, nil
		},
	}, nil)
	if _, err = s.QueueVerification(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	if n, e := s.ProcessVerification(ctx, 10); e != nil || n != 1 {
		t.Fatalf("verify %d %v", n, e)
	}
	c, err = s.Get(ctx, channelmarket.Actor{UserID: 1}, c.ID)
	if err != nil || c.Verification != "failed" || len(c.ModelResults) != 1 || c.ModelResults[0].Listed || c.ModelResults[0].Status != "failed" || c.Stage != "completed" || c.CompletedAt == nil || c.ModelResults[0].TestedAt.IsZero() {
		t.Fatalf("unlisted result %+v %v", c, err)
	}
	if err = s.Transition(ctx, channelmarket.Actor{UserID: 3, Admin: true}, c.InternalChannelID, "approve", ""); !errors.Is(err, channelmarket.ErrConflict) {
		t.Fatalf("published unlisted %v", err)
	}
	if _, err = s.Update(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, json.RawMessage(`{"model_consistency_status":"passed"}`)); !errors.Is(err, channelmarket.ErrInvalid) {
		t.Fatalf("owner annotation accepted %v", err)
	}
	c, err = s.Update(ctx, channelmarket.Actor{UserID: 3, Admin: true}, c.InternalChannelID, json.RawMessage(`{"model_consistency_status":"questionable"}`))
	if err != nil || c.ModelConsistencyStatus != "questionable" || c.Verification != "failed" {
		t.Fatalf("annotation changed connectivity %+v %v", c, err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE v3_channelmarket.verification_runs SET results='[{"model":"fixture-model","status":"failed","error":"fixture-hidden-upstream-secret"}]' WHERE channel_id=$1`, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	c, err = s.Get(ctx, channelmarket.Actor{UserID: 1}, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(c)
	if err != nil || bytes.Contains(body, []byte("fixture-hidden-upstream-secret")) {
		t.Fatalf("verification error leaked %s %v", body, err)
	}
}

func TestChangingApprovedSourceRequiresFreshReview(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.active(t, c)
	c, err := f.s.Update(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, json.RawMessage(`{"source_label":"new-source"}`))
	if err != nil || c.Status != "draft" || c.Verification != "pending" || c.SourceLabelStatus != "pending" || c.SubmittedSourceLabel != "new-source" {
		t.Fatalf("source claim bypassed review %+v %v", c, err)
	}
	if err = f.s.Transition(ctx, channelmarket.Actor{UserID: 3, Admin: true}, c.InternalChannelID, "approve", ""); !errors.Is(err, channelmarket.ErrConflict) {
		t.Fatalf("approved changed source without verification %v", err)
	}
	f.active(t, c)
	c, err = f.s.Get(ctx, channelmarket.Actor{UserID: 1}, c.ID)
	if err != nil || c.SourceLabelStatus != "approved" {
		t.Fatalf("source not reviewed %+v %v", c, err)
	}
	c, err = f.s.Update(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, json.RawMessage(`{"source_label":"new-source","name":"name-only"}`))
	if err != nil || c.Status != "active" {
		t.Fatalf("unchanged source forced review %+v %v", c, err)
	}
}
