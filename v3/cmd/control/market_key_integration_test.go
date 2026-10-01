//go:build pgintegration

package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/internal/identity"
)

func verifyMarketKeyAssembly(t *testing.T, pool *pgxpool.Pool, call apiCall, session string, userID int64) {
	t.Helper()
	w := call("POST", "/api/marketplace/route-pools", session, `{"name":"Control pool","members":[]}`, 200)
	var route struct {
		Data channelmarket.RoutePool `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &route); err != nil || route.Data.ID == "" {
		t.Fatalf("route pool creation failed: %v", err)
	}
	w = call("POST", "/api/marketplace/route-pools/"+route.Data.ID+"/bind-token", session, `{"token_id":0}`, 200)
	var bound struct {
		Data channelmarket.BoundToken `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &bound); err != nil || bound.Data.TokenID <= 0 || !strings.HasPrefix(bound.Data.APIKey, "sk-") {
		t.Fatalf("bound key not issued: %v", err)
	}
	var owner int64
	var group string
	if err := pool.QueryRow(context.Background(), `SELECT user_id,group_name FROM v3_identity.api_keys WHERE id=$1`, bound.Data.TokenID).Scan(&owner, &group); err != nil || owner != userID || group != route.Data.TokenGroup || bound.Data.TokenGroup != group {
		t.Fatalf("bound key ownership/group lost: owner=%d group=%q err=%v", owner, group, err)
	}
	call("GET", "/api/log/token", bound.Data.APIKey, "", 200)
	call("GET", "/api/wallet", bound.Data.APIKey, "", 401)
	w = call("POST", "/api/user/register", "", `{"username":"market_other","password":"strong-password"}`, 200)
	var other struct {
		Data identity.Session `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &other); err != nil || other.Data.AccessToken == "" {
		t.Fatalf("foreign user registration failed: %v", err)
	}
	call("POST", "/api/marketplace/route-pools/"+route.Data.ID+"/bind-token", other.Data.AccessToken, `{"token_id":0}`, 404)
}
