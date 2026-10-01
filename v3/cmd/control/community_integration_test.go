//go:build pgintegration

package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/community"
	"github.com/sh2001sh/new-api/v3/internal/identity"
)

func verifyCommunityFacade(t *testing.T, pool *pgxpool.Pool, call apiCall, session string) {
	t.Helper()
	ctx := context.Background()
	register := func(username string) identity.Session {
		t.Helper()
		w := call("POST", "/api/user/register", "", `{"username":"`+username+`","password":"strong-password"}`, 200)
		var response struct {
			Data identity.Session `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Data.AccessToken == "" {
			t.Fatalf("community registration: %v %s", err, w.Body)
		}
		return response.Data
	}
	owner, viewer := register("community_owner"), register("community_new")
	call("POST", "/api/marketplace/channels", owner.AccessToken, `{"provider_type":"openai_compatible","base_url":"https://api.test","api_key":"fixture-secret","declared_models":["test-model"],"visibility":"public"}`, 200)
	var ownerSubject string
	if err := pool.QueryRow(ctx, `SELECT external_id FROM v3_identity.users WHERE id=$1`, owner.User.ID).Scan(&ownerSubject); err != nil || len(ownerSubject) != 6 {
		t.Fatalf("new channel owner lacks public subject: %q %v", ownerSubject, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_catalog.channels(name,provider,base_url,status,scope,owner_user_id,settings)
 VALUES('community channel','openai','https://api.test','enabled','marketplace',$1,'{"community":{"id":"community-control","slug":"control","visibility":"public","verification_status":"passed","lifecycle_status":"active"}}')`, owner.User.ID); err != nil {
		t.Fatal(err)
	}
	call("GET", "/api/community/sellers", "", "", 401)
	w := call("GET", "/api/community/sellers", session, "", 200)
	var sellers struct {
		Data community.SellerList `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &sellers); err != nil || sellers.Data.Total != 1 || len(sellers.Data.Items) != 1 || sellers.Data.Items[0].Subject != ownerSubject {
		t.Fatalf("browser sellers: %v %s", err, w.Body)
	}
	call("GET", "/api/community/sellers?page_size=51", session, "", 400)
	call("POST", "/api/community/channels/community-control/rating", "", `{"stars":5}`, 401)
	call("POST", "/api/community/channels/community-control/rating", viewer.AccessToken, `{"stars":5,"viewer_sub":"ABC234"}`, 400)
	call("POST", "/api/community/channels/community-control/rating", viewer.AccessToken, `{"stars":0}`, 400)
	call("POST", "/api/community/channels/community-control/rating", viewer.AccessToken, `{"stars":5}`, 200)
	call("POST", "/api/community/channels/community-control/rating", viewer.AccessToken, `{"stars":3}`, 200)
	call("POST", "/api/community/channels/community-control/rating", owner.AccessToken, `{"stars":5}`, 403)
	call("POST", "/api/community/channels/nonexistent/rating", viewer.AccessToken, `{"stars":5}`, 404)
	var subject string
	var stars int
	var count int
	if err := pool.QueryRow(ctx, `SELECT external_id FROM v3_identity.users WHERE id=$1`, viewer.User.ID).Scan(&subject); err != nil || len(subject) != 6 {
		t.Fatalf("missing persisted session subject: %q %v", subject, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*),max(stars) FROM v3_community.channel_ratings WHERE channel_id='community-control' AND user_id=$1`, viewer.User.ID).Scan(&count, &stars); err != nil || count != 1 || stars != 3 {
		t.Fatalf("rating identity/idempotency: count=%d stars=%d err=%v", count, stars, err)
	}
}
