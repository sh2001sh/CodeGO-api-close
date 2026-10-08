//go:build pgintegration

package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/internal/community"
	"github.com/sh2001sh/new-api/v3/internal/identity"
)

func verifyCommunityFacade(t *testing.T, pool *pgxpool.Pool, call apiCall, session string) {
	t.Helper()
	ctx := context.Background()
	register := func(username string) identity.Session {
		t.Helper()
		w := call("POST", "/api/user/register", "", `{"username":"`+username+`","password":"strong-password","accepted_terms_version":"2026-10-07","accepted_privacy_version":"2026-10-07","agreement_locale":"en"}`, 200)
		var response struct {
			Data identity.Session `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Data.AccessToken == "" {
			t.Fatalf("community registration: %v %s", err, w.Body)
		}
		return response.Data
	}
	owner, viewer := register("community_owner"), register("community_new")
	call("POST", "/api/user/policy-acceptance", owner.AccessToken, `{"document":"supplier","version":"2026-10-07","locale":"en"}`, 200)
	w := call("POST", "/api/marketplace/channels", owner.AccessToken, `{"provider_type":"openai_compatible","base_url":"https://api.test","api_key":"fixture-secret","declared_models":["test-model"],"visibility":"public"}`, 200)
	var created struct {
		Data channelmarket.ChannelView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	c := created.Data
	var ownerSubject string
	if err := pool.QueryRow(ctx, `SELECT external_id FROM v3_identity.users WHERE id=$1`, owner.User.ID).Scan(&ownerSubject); err != nil || len(ownerSubject) != 6 {
		t.Fatalf("new channel owner lacks public subject: %q %v", ownerSubject, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET lifecycle_status='active',verification_status='passed' WHERE id=$1`, c.GroupID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_catalog.channels SET status='enabled',settings=jsonb_set(jsonb_set(settings,'{community,lifecycle_status}','"active"'),'{community,verification_status}','"passed"') WHERE id=$1`, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	call("GET", "/api/community/sellers", "", "", 401)
	w = call("GET", "/api/community/sellers", session, "", 200)
	var sellers struct {
		Data community.SellerList `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &sellers); err != nil || sellers.Data.Total != 1 || len(sellers.Data.Items) != 1 || sellers.Data.Items[0].Subject != ownerSubject {
		t.Fatalf("browser sellers: %v %s", err, w.Body)
	}
	call("GET", "/api/community/sellers?page_size=51", session, "", 400)
	path := "/api/marketplace/groups/" + c.ID + "/rating"
	call("GET", path, "", "", 200)
	call("POST", path, "", `{"stars":5}`, 401)
	call("POST", path, viewer.AccessToken, `{"stars":5,"viewer_sub":"ABC234"}`, 400)
	call("POST", path, viewer.AccessToken, `{"stars":0}`, 400)
	call("POST", path, viewer.AccessToken, `{"stars":5}`, 403)
	call("GET", "/api/wallet", viewer.AccessToken, "", 200)
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,request_id,terminal) SELECT now(),id,$1,$2,0,'community-control-use','completed' FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=$1 AND kind='wallet'`, viewer.User.ID, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	call("POST", path, viewer.AccessToken, `{"stars":5}`, 200)
	call("POST", "/api/community/channels/"+c.ID+"/rating", viewer.AccessToken, `{"stars":3}`, 200)
	call("POST", path, owner.AccessToken, `{"stars":5}`, 403)
	call("POST", "/api/community/channels/nonexistent/rating", viewer.AccessToken, `{"stars":5}`, 404)
	var subject string
	var stars int
	var count int
	if err := pool.QueryRow(ctx, `SELECT external_id FROM v3_identity.users WHERE id=$1`, viewer.User.ID).Scan(&subject); err != nil || len(subject) != 6 {
		t.Fatalf("missing persisted session subject: %q %v", subject, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*),max(stars) FROM v3_community.channel_ratings WHERE channel_id=$2 AND user_id=$1`, viewer.User.ID, c.ID).Scan(&count, &stars); err != nil || count != 1 || stars != 3 {
		t.Fatalf("rating identity/idempotency: count=%d stars=%d err=%v", count, stars, err)
	}
}
